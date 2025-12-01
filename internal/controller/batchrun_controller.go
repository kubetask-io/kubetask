// Copyright Contributors to the KubeTask project

// Package controller implements Kubernetes controllers for KubeTask resources
package controller

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kubetaskv1alpha1 "github.com/xuezhaojun/kubetask/api/v1alpha1"
)

// BatchRunReconciler reconciles a BatchRun object
type BatchRunReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kubetask.io,resources=batchruns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubetask.io,resources=batchruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubetask.io,resources=batchruns/finalizers,verbs=update
// +kubebuilder:rbac:groups=kubetask.io,resources=batches,verbs=get;list;watch
// +kubebuilder:rbac:groups=kubetask.io,resources=tasks,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *BatchRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// 1. Get BatchRun CR
	batchRun := &kubetaskv1alpha1.BatchRun{}
	if err := r.Get(ctx, req.NamespacedName, batchRun); err != nil {
		if errors.IsNotFound(err) {
			// BatchRun deleted, nothing to do
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch BatchRun")
		return ctrl.Result{}, err
	}

	// 2. If new, initialize status
	if batchRun.Status.Phase == "" {
		return r.initializeBatchRun(ctx, batchRun)
	}

	// 3. If completed/failed, skip
	if batchRun.Status.Phase == kubetaskv1alpha1.BatchRunPhaseSucceeded ||
		batchRun.Status.Phase == kubetaskv1alpha1.BatchRunPhaseFailed {
		return ctrl.Result{}, nil
	}

	// 4. Ensure Tasks are created for pending tasks
	if err := r.ensureTasks(ctx, batchRun); err != nil {
		log.Error(err, "unable to ensure tasks")
		return ctrl.Result{}, err
	}

	// 5. Update task status from Task CR status
	if err := r.updateTaskStatus(ctx, batchRun); err != nil {
		log.Error(err, "unable to update task status")
		return ctrl.Result{}, err
	}

	// 6. Update BatchRun overall status
	if err := r.updateBatchRunStatus(ctx, batchRun); err != nil {
		log.Error(err, "unable to update BatchRun status")
		return ctrl.Result{}, err
	}

	// 7. Requeue if tasks are still running
	if batchRun.Status.Progress.Running > 0 || batchRun.Status.Progress.Pending > 0 {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

// initializeBatchRun initializes a new BatchRun
func (r *BatchRunReconciler) initializeBatchRun(ctx context.Context, batchRun *kubetaskv1alpha1.BatchRun) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Get Batch spec (either from ref or inline)
	var batchSpec *kubetaskv1alpha1.BatchSpec
	switch {
	case batchRun.Spec.BatchRef != "":
		// Get referenced Batch
		batch := &kubetaskv1alpha1.Batch{}
		batchKey := types.NamespacedName{
			Name:      batchRun.Spec.BatchRef,
			Namespace: batchRun.Namespace,
		}
		if err := r.Get(ctx, batchKey, batch); err != nil {
			log.Error(err, "unable to fetch Batch", "batchRef", batchRun.Spec.BatchRef)
			return ctrl.Result{}, err
		}
		batchSpec = &batch.Spec
	case batchRun.Spec.BatchSpec != nil:
		// Use inline batch spec
		batchSpec = batchRun.Spec.BatchSpec
	default:
		err := fmt.Errorf("neither batchRef nor batchSpec specified")
		log.Error(err, "invalid BatchRun spec")
		return ctrl.Result{}, err
	}

	// Generate task list from commonContext + variableContexts
	// Task[i] = commonContext (constant) + variableContexts[i] (variable)
	tasks := []kubetaskv1alpha1.TaskStatus{}
	for _, variableContext := range batchSpec.VariableContexts {
		// Merge common and variable contexts
		taskContexts := append([]kubetaskv1alpha1.Context{}, batchSpec.CommonContext...)
		taskContexts = append(taskContexts, variableContext...)

		tasks = append(tasks, kubetaskv1alpha1.TaskStatus{
			Contexts: taskContexts,
			Status:   kubetaskv1alpha1.TaskPhasePending,
		})
	}

	// Initialize status
	now := metav1.Now()
	batchRun.Status.Phase = kubetaskv1alpha1.BatchRunPhasePending
	batchRun.Status.StartTime = &now
	batchRun.Status.Progress = kubetaskv1alpha1.ProgressStatus{
		Total:   len(tasks),
		Pending: len(tasks),
	}
	batchRun.Status.Tasks = tasks

	// Update CR status
	if err := r.Status().Update(ctx, batchRun); err != nil {
		log.Error(err, "unable to update BatchRun status")
		return ctrl.Result{}, err
	}

	log.Info("initialized BatchRun", "tasks", len(tasks))
	return ctrl.Result{Requeue: true}, nil
}

// ensureTasks creates Task CRs for pending tasks in the BatchRun
func (r *BatchRunReconciler) ensureTasks(ctx context.Context, batchRun *kubetaskv1alpha1.BatchRun) error {
	log := log.FromContext(ctx)

	for i, taskStatus := range batchRun.Status.Tasks {
		if taskStatus.Status != kubetaskv1alpha1.TaskPhasePending {
			continue
		}

		// Generate Task name using batch run name + task index
		taskName := fmt.Sprintf("%s-task-%d", batchRun.Name, i)

		// Check if Task already exists
		existingTask := &kubetaskv1alpha1.Task{}
		taskKey := types.NamespacedName{Name: taskName, Namespace: batchRun.Namespace}
		if err := r.Get(ctx, taskKey, existingTask); err == nil {
			// Task already exists, mark it as created in our status
			batchRun.Status.Tasks[i].Status = kubetaskv1alpha1.TaskPhaseRunning
			now := metav1.Now()
			batchRun.Status.Tasks[i].StartTime = &now
			batchRun.Status.Progress.Pending--
			batchRun.Status.Progress.Running++
			continue
		}

		// Get workspaceConfigRef from BatchSpec
		var workspaceConfigRef string
		if batchRun.Spec.BatchSpec != nil {
			workspaceConfigRef = batchRun.Spec.BatchSpec.WorkspaceConfigRef
		} else if batchRun.Spec.BatchRef != "" {
			// Get from referenced Batch
			batch := &kubetaskv1alpha1.Batch{}
			batchKey := types.NamespacedName{Name: batchRun.Spec.BatchRef, Namespace: batchRun.Namespace}
			if err := r.Get(ctx, batchKey, batch); err == nil {
				workspaceConfigRef = batch.Spec.WorkspaceConfigRef
			}
		}

		// Create Task CR
		task := &kubetaskv1alpha1.Task{
			ObjectMeta: metav1.ObjectMeta{
				Name:      taskName,
				Namespace: batchRun.Namespace,
				Labels: map[string]string{
					"kubetask.io/batch-run": batchRun.Name,
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: batchRun.APIVersion,
						Kind:       batchRun.Kind,
						Name:       batchRun.Name,
						UID:        batchRun.UID,
						Controller: boolPtr(true),
					},
				},
			},
			Spec: kubetaskv1alpha1.TaskSpec{
				Contexts:           taskStatus.Contexts,
				WorkspaceConfigRef: workspaceConfigRef,
			},
		}

		if err := r.Create(ctx, task); err != nil {
			log.Error(err, "unable to create Task", "task", taskName)
			return err
		}

		// Update task status to Running
		batchRun.Status.Tasks[i].Status = kubetaskv1alpha1.TaskPhaseRunning
		now := metav1.Now()
		batchRun.Status.Tasks[i].StartTime = &now
		batchRun.Status.Progress.Pending--
		batchRun.Status.Progress.Running++

		log.Info("created Task", "task", taskName, "taskIndex", i)
	}

	// Update BatchRun phase to Running
	if batchRun.Status.Progress.Running > 0 {
		batchRun.Status.Phase = kubetaskv1alpha1.BatchRunPhaseRunning
	}

	return r.Status().Update(ctx, batchRun)
}

// updateTaskStatus syncs task status from Task CR status
func (r *BatchRunReconciler) updateTaskStatus(ctx context.Context, batchRun *kubetaskv1alpha1.BatchRun) error {
	log := log.FromContext(ctx)

	changed := false
	for i, taskStatus := range batchRun.Status.Tasks {
		if taskStatus.Status != kubetaskv1alpha1.TaskPhaseRunning {
			continue
		}

		// Get Task name
		taskName := fmt.Sprintf("%s-task-%d", batchRun.Name, i)

		// Get Task CR status
		task := &kubetaskv1alpha1.Task{}
		taskKey := types.NamespacedName{Name: taskName, Namespace: batchRun.Namespace}
		if err := r.Get(ctx, taskKey, task); err != nil {
			if errors.IsNotFound(err) {
				log.Error(err, "Task not found", "task", taskName)
				continue
			}
			return err
		}

		// Check Task completion
		switch task.Status.Phase {
		case kubetaskv1alpha1.TaskPhaseSucceeded:
			batchRun.Status.Tasks[i].Status = kubetaskv1alpha1.TaskPhaseSucceeded
			if task.Status.CompletionTime != nil {
				batchRun.Status.Tasks[i].CompletionTime = task.Status.CompletionTime
			}
			batchRun.Status.Progress.Running--
			batchRun.Status.Progress.Completed++
			changed = true
			log.Info("task succeeded", "task", taskName)
		case kubetaskv1alpha1.TaskPhaseFailed:
			batchRun.Status.Tasks[i].Status = kubetaskv1alpha1.TaskPhaseFailed
			if task.Status.CompletionTime != nil {
				batchRun.Status.Tasks[i].CompletionTime = task.Status.CompletionTime
			}
			batchRun.Status.Progress.Running--
			batchRun.Status.Progress.Failed++
			changed = true
			log.Info("task failed", "task", taskName)
		}
	}

	if changed {
		return r.Status().Update(ctx, batchRun)
	}

	return nil
}

// updateBatchRunStatus updates the overall BatchRun status
func (r *BatchRunReconciler) updateBatchRunStatus(ctx context.Context, batchRun *kubetaskv1alpha1.BatchRun) error {
	// If all tasks completed
	if batchRun.Status.Progress.Pending == 0 && batchRun.Status.Progress.Running == 0 {
		now := metav1.Now()
		batchRun.Status.CompletionTime = &now

		if batchRun.Status.Progress.Failed > 0 {
			batchRun.Status.Phase = kubetaskv1alpha1.BatchRunPhaseFailed
		} else {
			batchRun.Status.Phase = kubetaskv1alpha1.BatchRunPhaseSucceeded
		}

		return r.Status().Update(ctx, batchRun)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BatchRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubetaskv1alpha1.BatchRun{}).
		Owns(&kubetaskv1alpha1.Task{}).
		Complete(r)
}

func boolPtr(b bool) *bool {
	return &b
}
