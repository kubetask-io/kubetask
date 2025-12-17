// Copyright Contributors to the KubeTask project

// Package controller implements Kubernetes controllers for KubeTask resources
package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
)

const (
	// CronWorkflowLabelKey is the label key used to identify WorkflowRuns created by a CronWorkflow
	CronWorkflowLabelKey = "kubetask.io/cronworkflow"

	// CronWorkflowScheduledTimeAnnotation is the annotation key for the scheduled time
	CronWorkflowScheduledTimeAnnotation = "kubetask.io/scheduled-at"
)

// Clock interface for testing time-sensitive operations
type Clock interface {
	Now() time.Time
}

// realClock implements Clock using the real time
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// CronWorkflowReconciler reconciles a CronWorkflow object
type CronWorkflowReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Clock  // for testing
}

// +kubebuilder:rbac:groups=kubetask.io,resources=cronworkflows,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubetask.io,resources=cronworkflows/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubetask.io,resources=cronworkflows/finalizers,verbs=update
// +kubebuilder:rbac:groups=kubetask.io,resources=workflows,verbs=get;list;watch
// +kubebuilder:rbac:groups=kubetask.io,resources=workflowruns,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop
func (r *CronWorkflowReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Use real clock if not set (for testing)
	if r.Clock == nil {
		r.Clock = realClock{}
	}

	// Get CronWorkflow CR
	cronWorkflow := &kubetaskv1alpha1.CronWorkflow{}
	if err := r.Get(ctx, req.NamespacedName, cronWorkflow); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch CronWorkflow")
		return ctrl.Result{}, err
	}

	// Get all child WorkflowRuns for this CronWorkflow
	childRuns, err := r.getChildWorkflowRuns(ctx, cronWorkflow)
	if err != nil {
		log.Error(err, "unable to list child WorkflowRuns")
		return ctrl.Result{}, err
	}

	// Categorize runs by status
	var activeRuns, successfulRuns []*kubetaskv1alpha1.WorkflowRun
	for i := range childRuns {
		run := &childRuns[i]
		switch run.Status.Phase {
		case kubetaskv1alpha1.WorkflowPhaseCompleted:
			successfulRuns = append(successfulRuns, run)
		case kubetaskv1alpha1.WorkflowPhaseFailed:
			// Failed runs are tracked but not used for scheduling decisions
			// (CronWorkflow uses Forbid policy - only active runs matter)
		case kubetaskv1alpha1.WorkflowPhaseRunning, kubetaskv1alpha1.WorkflowPhasePending:
			activeRuns = append(activeRuns, run)
		default:
			// New run without phase yet, consider it active
			activeRuns = append(activeRuns, run)
		}
	}

	// Update last successful time if there are completed runs
	if len(successfulRuns) > 0 {
		// Find the most recent completion
		var latestCompletion *metav1.Time
		for _, run := range successfulRuns {
			if run.Status.CompletionTime != nil {
				if latestCompletion == nil || run.Status.CompletionTime.After(latestCompletion.Time) {
					latestCompletion = run.Status.CompletionTime
				}
			}
		}
		if latestCompletion != nil {
			cronWorkflow.Status.LastSuccessfulTime = latestCompletion
		}
	}

	// Update active run references in status
	activeRefs := make([]corev1.ObjectReference, len(activeRuns))
	for i, run := range activeRuns {
		activeRefs[i] = corev1.ObjectReference{
			APIVersion: run.APIVersion,
			Kind:       run.Kind,
			Name:       run.Name,
			Namespace:  run.Namespace,
			UID:        run.UID,
		}
	}
	cronWorkflow.Status.Active = activeRefs

	// Check if suspended
	if cronWorkflow.Spec.Suspend != nil && *cronWorkflow.Spec.Suspend {
		log.V(1).Info("CronWorkflow is suspended, skipping scheduling")
		cronWorkflow.Status.ObservedGeneration = cronWorkflow.Generation
		if err := r.Status().Update(ctx, cronWorkflow); err != nil {
			log.Error(err, "unable to update CronWorkflow status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Parse cron schedule
	schedule, err := cron.ParseStandard(cronWorkflow.Spec.Schedule)
	if err != nil {
		log.Error(err, "invalid cron schedule", "schedule", cronWorkflow.Spec.Schedule)
		cronWorkflow.Status.ObservedGeneration = cronWorkflow.Generation
		meta.SetStatusCondition(&cronWorkflow.Status.Conditions, metav1.Condition{
			Type:    "Scheduled",
			Status:  metav1.ConditionFalse,
			Reason:  "InvalidSchedule",
			Message: fmt.Sprintf("Invalid cron schedule: %v", err),
		})
		if updateErr := r.Status().Update(ctx, cronWorkflow); updateErr != nil {
			log.Error(updateErr, "unable to update CronWorkflow status")
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil // Don't requeue, user needs to fix schedule
	}

	// Calculate next scheduled time
	now := r.Now()
	scheduledTime := r.getNextScheduleTime(cronWorkflow, now, schedule)

	// Check if we need to create a new WorkflowRun
	if scheduledTime != nil {
		// Check for concurrent runs - CronWorkflow always uses Forbid policy (no history cleanup needed)
		if len(activeRuns) > 0 {
			log.V(1).Info("active WorkflowRun exists, skipping scheduling", "active", len(activeRuns))
			cronWorkflow.Status.ObservedGeneration = cronWorkflow.Generation
			if err := r.Status().Update(ctx, cronWorkflow); err != nil {
				log.Error(err, "unable to update CronWorkflow status")
				return ctrl.Result{}, err
			}
			return r.requeueForNextSchedule(now, schedule)
		}

		// Create new WorkflowRun
		run, err := r.createWorkflowRun(ctx, cronWorkflow, *scheduledTime)
		if err != nil {
			log.Error(err, "unable to create WorkflowRun")
			return ctrl.Result{}, err
		}

		log.Info("created WorkflowRun", "workflowRun", run.Name, "scheduledTime", scheduledTime)

		// Update last schedule time
		cronWorkflow.Status.LastScheduleTime = &metav1.Time{Time: *scheduledTime}

		// Update condition
		meta.SetStatusCondition(&cronWorkflow.Status.Conditions, metav1.Condition{
			Type:    "Scheduled",
			Status:  metav1.ConditionTrue,
			Reason:  "WorkflowRunCreated",
			Message: fmt.Sprintf("Created WorkflowRun %s", run.Name),
		})
	}

	// Update status
	cronWorkflow.Status.ObservedGeneration = cronWorkflow.Generation
	if err := r.Status().Update(ctx, cronWorkflow); err != nil {
		log.Error(err, "unable to update CronWorkflow status")
		return ctrl.Result{}, err
	}

	// Requeue for next schedule
	return r.requeueForNextSchedule(now, schedule)
}

// getChildWorkflowRuns returns all WorkflowRuns owned by this CronWorkflow
func (r *CronWorkflowReconciler) getChildWorkflowRuns(ctx context.Context, cronWorkflow *kubetaskv1alpha1.CronWorkflow) ([]kubetaskv1alpha1.WorkflowRun, error) {
	runList := &kubetaskv1alpha1.WorkflowRunList{}
	if err := r.List(ctx, runList, client.InNamespace(cronWorkflow.Namespace), client.MatchingLabels{
		CronWorkflowLabelKey: cronWorkflow.Name,
	}); err != nil {
		return nil, err
	}
	return runList.Items, nil
}

// getNextScheduleTime calculates the next scheduled time
func (r *CronWorkflowReconciler) getNextScheduleTime(cronWorkflow *kubetaskv1alpha1.CronWorkflow, now time.Time, schedule cron.Schedule) *time.Time {
	var lastScheduleTime time.Time
	if cronWorkflow.Status.LastScheduleTime != nil {
		lastScheduleTime = cronWorkflow.Status.LastScheduleTime.Time
	} else {
		// Use creation time as the starting point
		lastScheduleTime = cronWorkflow.CreationTimestamp.Time
	}

	// If lastScheduleTime is in the future (clock skew), use creation time
	if lastScheduleTime.After(now) {
		lastScheduleTime = cronWorkflow.CreationTimestamp.Time
	}

	// Check if we should run now (if the last schedule time is before now)
	scheduledTime := schedule.Next(lastScheduleTime)
	if scheduledTime.Before(now) || scheduledTime.Equal(now) {
		return &scheduledTime
	}

	return nil
}

// requeueForNextSchedule calculates when to requeue for the next scheduled run
func (r *CronWorkflowReconciler) requeueForNextSchedule(now time.Time, schedule cron.Schedule) (ctrl.Result, error) {
	nextRun := schedule.Next(now)
	requeueAfter := nextRun.Sub(now)

	// Add a small buffer to ensure we don't requeue too early
	if requeueAfter < time.Second {
		requeueAfter = time.Second
	}

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// createWorkflowRun creates a new WorkflowRun from the CronWorkflow spec
func (r *CronWorkflowReconciler) createWorkflowRun(ctx context.Context, cronWorkflow *kubetaskv1alpha1.CronWorkflow, scheduledTime time.Time) (*kubetaskv1alpha1.WorkflowRun, error) {
	// Generate unique run name using timestamp
	runName := fmt.Sprintf("%s-%d", cronWorkflow.Name, scheduledTime.Unix())

	// Build WorkflowRunSpec based on CronWorkflow spec
	runSpec := kubetaskv1alpha1.WorkflowRunSpec{}
	if cronWorkflow.Spec.WorkflowRef != "" {
		runSpec.WorkflowRef = cronWorkflow.Spec.WorkflowRef
	} else if cronWorkflow.Spec.Inline != nil {
		runSpec.Inline = cronWorkflow.Spec.Inline.DeepCopy()
	} else {
		return nil, fmt.Errorf("CronWorkflow must specify either workflowRef or inline")
	}

	// Create WorkflowRun
	run := &kubetaskv1alpha1.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      runName,
			Namespace: cronWorkflow.Namespace,
			Labels: map[string]string{
				CronWorkflowLabelKey: cronWorkflow.Name,
			},
			Annotations: map[string]string{
				CronWorkflowScheduledTimeAnnotation: scheduledTime.Format(time.RFC3339),
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: kubetaskv1alpha1.GroupVersion.String(),
					Kind:       "CronWorkflow",
					Name:       cronWorkflow.Name,
					UID:        cronWorkflow.UID,
					Controller: boolPtr(true),
				},
			},
		},
		Spec: runSpec,
	}

	if err := r.Create(ctx, run); err != nil {
		return nil, err
	}

	return run, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *CronWorkflowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubetaskv1alpha1.CronWorkflow{}).
		Owns(&kubetaskv1alpha1.WorkflowRun{}).
		Complete(r)
}
