// Copyright Contributors to the KubeTask project

// Package controller implements Kubernetes controllers for KubeTask resources
package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
)

// Label and annotation keys are defined in workflowrun_controller.go
// These are kept here for backward compatibility during migration
const (
	// WorkflowLabelKey is the label key used to identify Tasks created by a Workflow.
	//
	// Deprecated: Use WorkflowRunLabelKey from workflowrun_controller.go
	WorkflowLabelKey = "kubetask.io/workflow"

	// WorkflowStageLabelKey is the label key for the stage name.
	//
	// Deprecated: Use WorkflowRunStageLabelKey from workflowrun_controller.go
	WorkflowStageLabelKey = "kubetask.io/stage"

	// WorkflowStageIndexLabelKey is the label key for the stage index (for ordering).
	//
	// Deprecated: Use WorkflowRunStageIndexLabelKey from workflowrun_controller.go
	WorkflowStageIndexLabelKey = "kubetask.io/stage-index"

	// WorkflowDependsOnAnnotation is the annotation for task dependencies.
	//
	// Deprecated: Use WorkflowRunDependsOnAnnotation from workflowrun_controller.go
	WorkflowDependsOnAnnotation = "kubetask.io/depends-on"
)

// WorkflowReconciler reconciles a Workflow object.
// Workflow is a template resource and does not require active reconciliation.
// The reconciler only validates and accepts Workflow resources.
// To execute a workflow, create a WorkflowRun that references this Workflow.
type WorkflowReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kubetask.io,resources=workflows,verbs=get;list;watch;create;update;patch;delete

// Reconcile validates Workflow resources.
// Since Workflow is a template, no active reconciliation is needed.
// Execution happens through WorkflowRun resources.
func (r *WorkflowReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Get Workflow CR to validate it exists
	workflow := &kubetaskv1alpha1.Workflow{}
	if err := r.Get(ctx, req.NamespacedName, workflow); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch Workflow")
		return ctrl.Result{}, err
	}

	// Workflow is a template - no active reconciliation needed
	// WorkflowRun resources reference Workflow templates and handle execution
	log.V(1).Info("workflow template reconciled", "name", workflow.Name)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *WorkflowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubetaskv1alpha1.Workflow{}).
		Complete(r)
}
