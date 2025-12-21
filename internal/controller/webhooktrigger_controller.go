// Copyright Contributors to the KubeTask project

package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
	"github.com/kubetask/kubetask/internal/webhook"
)

const (
	// WebhookTriggerLabelKey is the label key used to identify resources created by a WebhookTrigger
	WebhookTriggerLabelKey = "kubetask.io/webhook-trigger"

	// WebhookRuleLabelKey is the label key used to identify resources created by a specific rule
	WebhookRuleLabelKey = "kubetask.io/webhook-rule"

	// ResourceKindLabelKey is the label key used to identify the type of resource created
	ResourceKindLabelKey = "kubetask.io/resource-kind"
)

// WebhookTriggerReconciler reconciles a WebhookTrigger object
type WebhookTriggerReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	WebhookServer *webhook.Server
}

// +kubebuilder:rbac:groups=kubetask.io,resources=webhooktriggers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubetask.io,resources=webhooktriggers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubetask.io,resources=webhooktriggers/finalizers,verbs=update
// +kubebuilder:rbac:groups=kubetask.io,resources=tasks,verbs=get;list;watch
// +kubebuilder:rbac:groups=kubetask.io,resources=workflowruns,verbs=get;list;watch

// Reconcile handles WebhookTrigger events
func (r *WebhookTriggerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the WebhookTrigger
	trigger := &kubetaskv1alpha1.WebhookTrigger{}
	if err := r.Get(ctx, req.NamespacedName, trigger); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Unable to fetch WebhookTrigger")
			return ctrl.Result{}, err
		}
		// WebhookTrigger was deleted, unregister from webhook server
		if r.WebhookServer != nil {
			r.WebhookServer.UnregisterTrigger(req.Namespace, req.Name)
		}
		return ctrl.Result{}, nil
	}

	// Register/update the trigger in the webhook server
	if r.WebhookServer != nil {
		r.WebhookServer.RegisterTrigger(trigger)
	}

	// Update the webhook URL in status
	webhookURL := fmt.Sprintf("/webhooks/%s/%s", trigger.Namespace, trigger.Name)
	if trigger.Status.WebhookURL != webhookURL {
		trigger.Status.WebhookURL = webhookURL
	}

	// Update active resources list by checking which resources are still running
	activeResources, err := r.getActiveResources(ctx, trigger)
	if err != nil {
		logger.Error(err, "Failed to get active resources")
		// Don't fail the reconcile, just log the error
	} else {
		trigger.Status.ActiveResources = activeResources
		// Also update ActiveTasks for backward compatibility
		trigger.Status.ActiveTasks = activeResources
	}

	// Update per-rule statuses if using rules-based trigger
	if len(trigger.Spec.Rules) > 0 {
		ruleStatuses, err := r.getRuleStatuses(ctx, trigger)
		if err != nil {
			logger.Error(err, "Failed to get rule statuses")
		} else {
			trigger.Status.RuleStatuses = ruleStatuses
		}
	}

	// Set Ready condition
	meta.SetStatusCondition(&trigger.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "WebhookRegistered",
		Message:            fmt.Sprintf("Webhook endpoint registered at %s", webhookURL),
		ObservedGeneration: trigger.Generation,
	})

	trigger.Status.ObservedGeneration = trigger.Generation

	// Update status
	if err := r.Status().Update(ctx, trigger); err != nil {
		logger.Error(err, "Failed to update WebhookTrigger status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// getActiveResources returns the list of resources (Tasks and WorkflowRuns) that are still running for this trigger.
func (r *WebhookTriggerReconciler) getActiveResources(ctx context.Context, trigger *kubetaskv1alpha1.WebhookTrigger) ([]string, error) {
	var activeResources []string

	// List Tasks with the webhook trigger label
	taskList := &kubetaskv1alpha1.TaskList{}
	if err := r.List(ctx, taskList,
		client.InNamespace(trigger.Namespace),
		client.MatchingLabels{WebhookTriggerLabelKey: trigger.Name},
	); err != nil {
		return nil, err
	}

	for _, task := range taskList.Items {
		// Include tasks that are still active (running, pending, queued, or newly created with empty phase)
		// Exclude only completed or failed tasks
		if task.Status.Phase != kubetaskv1alpha1.TaskPhaseCompleted &&
			task.Status.Phase != kubetaskv1alpha1.TaskPhaseFailed {
			activeResources = append(activeResources, task.Name)
		}
	}

	// List WorkflowRuns with the webhook trigger label
	workflowRunList := &kubetaskv1alpha1.WorkflowRunList{}
	if err := r.List(ctx, workflowRunList,
		client.InNamespace(trigger.Namespace),
		client.MatchingLabels{WebhookTriggerLabelKey: trigger.Name},
	); err != nil {
		return nil, err
	}

	for _, wr := range workflowRunList.Items {
		// Include workflow runs that are still active
		if wr.Status.Phase != kubetaskv1alpha1.WorkflowPhaseCompleted &&
			wr.Status.Phase != kubetaskv1alpha1.WorkflowPhaseFailed {
			activeResources = append(activeResources, wr.Name)
		}
	}

	return activeResources, nil
}

// getRuleStatuses returns the per-rule status information.
func (r *WebhookTriggerReconciler) getRuleStatuses(ctx context.Context, trigger *kubetaskv1alpha1.WebhookTrigger) ([]kubetaskv1alpha1.WebhookRuleStatus, error) {
	// Build a map of existing rule statuses to preserve LastTriggeredTime and TotalTriggered
	existingStatuses := make(map[string]*kubetaskv1alpha1.WebhookRuleStatus)
	for i := range trigger.Status.RuleStatuses {
		existingStatuses[trigger.Status.RuleStatuses[i].Name] = &trigger.Status.RuleStatuses[i]
	}

	var ruleStatuses []kubetaskv1alpha1.WebhookRuleStatus

	for _, rule := range trigger.Spec.Rules {
		// Get active resources for this rule
		activeResources, err := r.getActiveResourcesForRule(ctx, trigger, rule.Name)
		if err != nil {
			return nil, err
		}

		// Build rule status
		status := kubetaskv1alpha1.WebhookRuleStatus{
			Name:            rule.Name,
			ActiveResources: activeResources,
		}

		// Preserve existing counters if present
		if existing, ok := existingStatuses[rule.Name]; ok {
			status.LastTriggeredTime = existing.LastTriggeredTime
			status.TotalTriggered = existing.TotalTriggered
		}

		ruleStatuses = append(ruleStatuses, status)
	}

	return ruleStatuses, nil
}

// getActiveResourcesForRule returns active resources for a specific rule.
func (r *WebhookTriggerReconciler) getActiveResourcesForRule(ctx context.Context, trigger *kubetaskv1alpha1.WebhookTrigger, ruleName string) ([]string, error) {
	var activeResources []string

	// List Tasks for this rule
	taskList := &kubetaskv1alpha1.TaskList{}
	if err := r.List(ctx, taskList,
		client.InNamespace(trigger.Namespace),
		client.MatchingLabels{
			WebhookTriggerLabelKey: trigger.Name,
			WebhookRuleLabelKey:    ruleName,
		},
	); err != nil {
		return nil, err
	}

	for _, task := range taskList.Items {
		if task.Status.Phase != kubetaskv1alpha1.TaskPhaseCompleted &&
			task.Status.Phase != kubetaskv1alpha1.TaskPhaseFailed {
			activeResources = append(activeResources, task.Name)
		}
	}

	// List WorkflowRuns for this rule
	workflowRunList := &kubetaskv1alpha1.WorkflowRunList{}
	if err := r.List(ctx, workflowRunList,
		client.InNamespace(trigger.Namespace),
		client.MatchingLabels{
			WebhookTriggerLabelKey: trigger.Name,
			WebhookRuleLabelKey:    ruleName,
		},
	); err != nil {
		return nil, err
	}

	for _, wr := range workflowRunList.Items {
		if wr.Status.Phase != kubetaskv1alpha1.WorkflowPhaseCompleted &&
			wr.Status.Phase != kubetaskv1alpha1.WorkflowPhaseFailed {
			activeResources = append(activeResources, wr.Name)
		}
	}

	return activeResources, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *WebhookTriggerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubetaskv1alpha1.WebhookTrigger{}).
		// Watch Tasks to update ActiveTasks in trigger status
		Owns(&kubetaskv1alpha1.Task{}).
		Complete(r)
}
