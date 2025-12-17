// Copyright Contributors to the KubeTask project

// Package controller implements Kubernetes controllers for KubeTask resources
package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

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
	// WorkflowRunLabelKey is the label key used to identify Tasks created by a WorkflowRun
	WorkflowRunLabelKey = "kubetask.io/workflow-run"

	// WorkflowRefLabelKey is the label key for the referenced Workflow template name
	WorkflowRefLabelKey = "kubetask.io/workflow"

	// WorkflowRunStageLabelKey is the label key for the stage name
	WorkflowRunStageLabelKey = "kubetask.io/stage"

	// WorkflowRunStageIndexLabelKey is the label key for the stage index (for ordering)
	WorkflowRunStageIndexLabelKey = "kubetask.io/stage-index"

	// WorkflowRunDependsOnAnnotation is the annotation for task dependencies
	// Contains comma-separated list of Task CR names from the previous stage
	WorkflowRunDependsOnAnnotation = "kubetask.io/depends-on"

	// DefaultWorkflowRunRequeueDelay is the default delay for requeuing workflow run checks
	DefaultWorkflowRunRequeueDelay = 5 * time.Second
)

// WorkflowRunReconciler reconciles a WorkflowRun object
type WorkflowRunReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kubetask.io,resources=workflowruns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubetask.io,resources=workflowruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubetask.io,resources=workflowruns/finalizers,verbs=update
// +kubebuilder:rbac:groups=kubetask.io,resources=workflows,verbs=get;list;watch
// +kubebuilder:rbac:groups=kubetask.io,resources=tasks,verbs=get;list;watch;create;update;patch;delete

// Reconcile is the main reconciliation loop for WorkflowRun
func (r *WorkflowRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Get WorkflowRun CR
	workflowRun := &kubetaskv1alpha1.WorkflowRun{}
	if err := r.Get(ctx, req.NamespacedName, workflowRun); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch WorkflowRun")
		return ctrl.Result{}, err
	}

	// State machine based on phase
	switch workflowRun.Status.Phase {
	case "":
		// New workflow run - initialize
		return r.initializeWorkflowRun(ctx, workflowRun)

	case kubetaskv1alpha1.WorkflowPhasePending:
		// Start first stage
		return r.startStage(ctx, workflowRun, 0)

	case kubetaskv1alpha1.WorkflowPhaseRunning:
		// Monitor current stage, advance if complete
		return r.monitorAndAdvance(ctx, workflowRun)

	case kubetaskv1alpha1.WorkflowPhaseCompleted, kubetaskv1alpha1.WorkflowPhaseFailed:
		// Terminal states - nothing to do
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// getWorkflowSpec resolves the WorkflowSpec from either workflowRef or inline
func (r *WorkflowRunReconciler) getWorkflowSpec(ctx context.Context, workflowRun *kubetaskv1alpha1.WorkflowRun) (*kubetaskv1alpha1.WorkflowSpec, string, error) {
	// Inline takes precedence
	if workflowRun.Spec.Inline != nil {
		return workflowRun.Spec.Inline, "", nil
	}

	// Resolve from workflowRef
	if workflowRun.Spec.WorkflowRef != "" {
		workflow := &kubetaskv1alpha1.Workflow{}
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: workflowRun.Namespace,
			Name:      workflowRun.Spec.WorkflowRef,
		}, workflow); err != nil {
			return nil, "", fmt.Errorf("failed to get Workflow %s: %w", workflowRun.Spec.WorkflowRef, err)
		}
		return &workflow.Spec, workflowRun.Spec.WorkflowRef, nil
	}

	return nil, "", fmt.Errorf("WorkflowRun must specify either workflowRef or inline")
}

// initializeWorkflowRun sets up initial status for a new WorkflowRun
func (r *WorkflowRunReconciler) initializeWorkflowRun(ctx context.Context, workflowRun *kubetaskv1alpha1.WorkflowRun) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("initializing workflow run", "workflowRun", workflowRun.Name)

	// Get workflow spec
	workflowSpec, _, err := r.getWorkflowSpec(ctx, workflowRun)
	if err != nil {
		log.Error(err, "failed to get workflow spec")
		return r.failWorkflowRun(ctx, workflowRun, err.Error())
	}

	// Calculate total tasks and initialize stage statuses
	totalTasks := int32(0)
	stageStatuses := make([]kubetaskv1alpha1.WorkflowStageStatus, len(workflowSpec.Stages))

	for i, stage := range workflowSpec.Stages {
		totalTasks += int32(len(stage.Tasks)) // #nosec G115 -- task count is bounded by workflow spec validation
		stageName := stage.Name
		if stageName == "" {
			stageName = fmt.Sprintf("stage-%d", i)
		}
		stageStatuses[i] = kubetaskv1alpha1.WorkflowStageStatus{
			Name:  stageName,
			Phase: kubetaskv1alpha1.WorkflowPhasePending,
		}
	}

	// Update status
	workflowRun.Status.ObservedGeneration = workflowRun.Generation
	workflowRun.Status.Phase = kubetaskv1alpha1.WorkflowPhasePending
	workflowRun.Status.CurrentStage = -1
	workflowRun.Status.TotalTasks = totalTasks
	workflowRun.Status.CompletedTasks = 0
	workflowRun.Status.FailedTasks = 0
	workflowRun.Status.StageStatuses = stageStatuses

	meta.SetStatusCondition(&workflowRun.Status.Conditions, metav1.Condition{
		Type:    "Initialized",
		Status:  metav1.ConditionTrue,
		Reason:  "WorkflowRunInitialized",
		Message: fmt.Sprintf("WorkflowRun initialized with %d stages and %d total tasks", len(workflowSpec.Stages), totalTasks),
	})

	if err := r.Status().Update(ctx, workflowRun); err != nil {
		log.Error(err, "unable to update WorkflowRun status")
		return ctrl.Result{}, err
	}

	// Requeue immediately to start first stage
	return ctrl.Result{Requeue: true}, nil
}

// startStage creates Tasks for the specified stage
func (r *WorkflowRunReconciler) startStage(ctx context.Context, workflowRun *kubetaskv1alpha1.WorkflowRun, stageIndex int) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Get workflow spec
	workflowSpec, workflowRef, err := r.getWorkflowSpec(ctx, workflowRun)
	if err != nil {
		log.Error(err, "failed to get workflow spec")
		return r.failWorkflowRun(ctx, workflowRun, err.Error())
	}

	// Check if all stages are complete
	if stageIndex >= len(workflowSpec.Stages) {
		return r.completeWorkflowRun(ctx, workflowRun)
	}

	stage := workflowSpec.Stages[stageIndex]
	stageName := stage.Name
	if stageName == "" {
		stageName = fmt.Sprintf("stage-%d", stageIndex)
	}

	log.Info("starting stage", "workflowRun", workflowRun.Name, "stage", stageName, "index", stageIndex)

	// Get previous stage task names for depends-on annotation
	var previousTaskNames []string
	if stageIndex > 0 && len(workflowRun.Status.StageStatuses) > stageIndex-1 {
		previousTaskNames = workflowRun.Status.StageStatuses[stageIndex-1].Tasks
	}

	// Create Tasks for this stage
	var createdTaskNames []string
	for _, wfTask := range stage.Tasks {
		taskName := fmt.Sprintf("%s-%s", workflowRun.Name, wfTask.Name)

		// Check if Task already exists
		existingTask := &kubetaskv1alpha1.Task{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: workflowRun.Namespace, Name: taskName}, existingTask); err == nil {
			// Task already exists
			createdTaskNames = append(createdTaskNames, taskName)
			continue
		}

		// Build labels
		labels := map[string]string{
			WorkflowRunLabelKey:           workflowRun.Name,
			WorkflowRunStageLabelKey:      stageName,
			WorkflowRunStageIndexLabelKey: strconv.Itoa(stageIndex),
		}

		// Add workflow reference label if using workflowRef
		if workflowRef != "" {
			labels[WorkflowRefLabelKey] = workflowRef
		}

		// Build annotations
		annotations := make(map[string]string)
		if len(previousTaskNames) > 0 {
			annotations[WorkflowRunDependsOnAnnotation] = strings.Join(previousTaskNames, ",")
		}

		task := &kubetaskv1alpha1.Task{
			ObjectMeta: metav1.ObjectMeta{
				Name:        taskName,
				Namespace:   workflowRun.Namespace,
				Labels:      labels,
				Annotations: annotations,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: kubetaskv1alpha1.GroupVersion.String(),
						Kind:       "WorkflowRun",
						Name:       workflowRun.Name,
						UID:        workflowRun.UID,
						Controller: boolPtr(true),
					},
				},
			},
			Spec: wfTask.Spec,
		}

		if err := r.Create(ctx, task); err != nil {
			if !errors.IsAlreadyExists(err) {
				log.Error(err, "failed to create task", "task", taskName)
				return ctrl.Result{}, err
			}
		}
		createdTaskNames = append(createdTaskNames, taskName)
		log.Info("created task", "task", taskName, "stage", stageName)
	}

	// Update status
	now := metav1.Now()
	if workflowRun.Status.StartTime == nil {
		workflowRun.Status.StartTime = &now
	}

	workflowRun.Status.Phase = kubetaskv1alpha1.WorkflowPhaseRunning
	workflowRun.Status.CurrentStage = int32(stageIndex) // #nosec G115 -- stage index is bounded by workflow spec stages count
	workflowRun.Status.StageStatuses[stageIndex].Phase = kubetaskv1alpha1.WorkflowPhaseRunning
	workflowRun.Status.StageStatuses[stageIndex].Tasks = createdTaskNames
	workflowRun.Status.StageStatuses[stageIndex].StartTime = &now

	meta.SetStatusCondition(&workflowRun.Status.Conditions, metav1.Condition{
		Type:    "StageRunning",
		Status:  metav1.ConditionTrue,
		Reason:  "TasksCreated",
		Message: fmt.Sprintf("Created %d tasks for stage %s", len(createdTaskNames), stageName),
	})

	if err := r.Status().Update(ctx, workflowRun); err != nil {
		log.Error(err, "unable to update WorkflowRun status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: DefaultWorkflowRunRequeueDelay}, nil
}

// monitorAndAdvance checks current stage status and advances if complete
func (r *WorkflowRunReconciler) monitorAndAdvance(ctx context.Context, workflowRun *kubetaskv1alpha1.WorkflowRun) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Get workflow spec
	workflowSpec, _, err := r.getWorkflowSpec(ctx, workflowRun)
	if err != nil {
		log.Error(err, "failed to get workflow spec")
		return r.failWorkflowRun(ctx, workflowRun, err.Error())
	}

	stageIndex := int(workflowRun.Status.CurrentStage)
	if stageIndex < 0 || stageIndex >= len(workflowRun.Status.StageStatuses) {
		log.Error(nil, "invalid current stage index", "index", stageIndex)
		return ctrl.Result{}, fmt.Errorf("invalid current stage index: %d", stageIndex)
	}

	stageName := workflowRun.Status.StageStatuses[stageIndex].Name

	// Get all Tasks for this stage
	taskList := &kubetaskv1alpha1.TaskList{}
	if err := r.List(ctx, taskList,
		client.InNamespace(workflowRun.Namespace),
		client.MatchingLabels{
			WorkflowRunLabelKey:           workflowRun.Name,
			WorkflowRunStageIndexLabelKey: strconv.Itoa(stageIndex),
		}); err != nil {
		log.Error(err, "unable to list tasks for stage")
		return ctrl.Result{}, err
	}

	// Count task states
	var completed, failed, running int
	for _, task := range taskList.Items {
		switch task.Status.Phase {
		case kubetaskv1alpha1.TaskPhaseCompleted:
			completed++
		case kubetaskv1alpha1.TaskPhaseFailed:
			failed++
		case kubetaskv1alpha1.TaskPhaseRunning, kubetaskv1alpha1.TaskPhasePending, kubetaskv1alpha1.TaskPhaseQueued:
			running++
		default:
			running++ // Treat unknown/empty as running
		}
	}

	log.V(1).Info("stage task status", "stage", stageName, "completed", completed, "failed", failed, "running", running)

	// Update workflow run task counts
	workflowRun.Status.CompletedTasks = r.countAllCompletedTasks(ctx, workflowRun)
	workflowRun.Status.FailedTasks = r.countAllFailedTasks(ctx, workflowRun)

	// Check for failure (Fail Fast)
	if failed > 0 {
		log.Info("task failed, failing workflow run", "stage", stageName, "failed", failed)
		return r.failWorkflowRun(ctx, workflowRun, fmt.Sprintf("Task failed in stage %s", stageName))
	}

	// Check if all tasks in current stage completed
	expectedTasks := len(workflowSpec.Stages[stageIndex].Tasks)
	if completed == expectedTasks {
		log.Info("stage completed", "stage", stageName, "completed", completed)
		now := metav1.Now()
		workflowRun.Status.StageStatuses[stageIndex].Phase = kubetaskv1alpha1.WorkflowPhaseCompleted
		workflowRun.Status.StageStatuses[stageIndex].CompletionTime = &now

		// Update status before advancing
		if err := r.Status().Update(ctx, workflowRun); err != nil {
			log.Error(err, "unable to update WorkflowRun status")
			return ctrl.Result{}, err
		}

		// Advance to next stage
		nextStage := stageIndex + 1
		if nextStage >= len(workflowSpec.Stages) {
			// All stages complete
			return r.completeWorkflowRun(ctx, workflowRun)
		}

		// Start next stage
		return r.startStage(ctx, workflowRun, nextStage)
	}

	// Still running, update status and requeue
	if err := r.Status().Update(ctx, workflowRun); err != nil {
		log.Error(err, "unable to update WorkflowRun status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: DefaultWorkflowRunRequeueDelay}, nil
}

// failWorkflowRun marks the workflow run as failed
func (r *WorkflowRunReconciler) failWorkflowRun(ctx context.Context, workflowRun *kubetaskv1alpha1.WorkflowRun, message string) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("failing workflow run", "workflowRun", workflowRun.Name, "message", message)

	now := metav1.Now()
	workflowRun.Status.Phase = kubetaskv1alpha1.WorkflowPhaseFailed
	workflowRun.Status.CompletionTime = &now

	// Mark current stage as failed
	if workflowRun.Status.CurrentStage >= 0 && int(workflowRun.Status.CurrentStage) < len(workflowRun.Status.StageStatuses) {
		workflowRun.Status.StageStatuses[workflowRun.Status.CurrentStage].Phase = kubetaskv1alpha1.WorkflowPhaseFailed
		workflowRun.Status.StageStatuses[workflowRun.Status.CurrentStage].CompletionTime = &now
	}

	meta.SetStatusCondition(&workflowRun.Status.Conditions, metav1.Condition{
		Type:    "Failed",
		Status:  metav1.ConditionTrue,
		Reason:  "TaskFailed",
		Message: message,
	})

	meta.SetStatusCondition(&workflowRun.Status.Conditions, metav1.Condition{
		Type:    "StageRunning",
		Status:  metav1.ConditionFalse,
		Reason:  "WorkflowRunFailed",
		Message: message,
	})

	if err := r.Status().Update(ctx, workflowRun); err != nil {
		log.Error(err, "unable to update WorkflowRun status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// completeWorkflowRun marks the workflow run as successfully completed
func (r *WorkflowRunReconciler) completeWorkflowRun(ctx context.Context, workflowRun *kubetaskv1alpha1.WorkflowRun) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("completing workflow run", "workflowRun", workflowRun.Name)

	now := metav1.Now()
	workflowRun.Status.Phase = kubetaskv1alpha1.WorkflowPhaseCompleted
	workflowRun.Status.CompletionTime = &now
	workflowRun.Status.CompletedTasks = workflowRun.Status.TotalTasks

	meta.SetStatusCondition(&workflowRun.Status.Conditions, metav1.Condition{
		Type:    "Completed",
		Status:  metav1.ConditionTrue,
		Reason:  "AllStagesCompleted",
		Message: "All workflow stages completed successfully",
	})

	meta.SetStatusCondition(&workflowRun.Status.Conditions, metav1.Condition{
		Type:    "StageRunning",
		Status:  metav1.ConditionFalse,
		Reason:  "WorkflowRunCompleted",
		Message: "All stages completed",
	})

	if err := r.Status().Update(ctx, workflowRun); err != nil {
		log.Error(err, "unable to update WorkflowRun status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// countAllCompletedTasks counts all completed tasks across all stages
func (r *WorkflowRunReconciler) countAllCompletedTasks(ctx context.Context, workflowRun *kubetaskv1alpha1.WorkflowRun) int32 {
	taskList := &kubetaskv1alpha1.TaskList{}
	if err := r.List(ctx, taskList,
		client.InNamespace(workflowRun.Namespace),
		client.MatchingLabels{WorkflowRunLabelKey: workflowRun.Name}); err != nil {
		return 0
	}

	count := int32(0)
	for _, task := range taskList.Items {
		if task.Status.Phase == kubetaskv1alpha1.TaskPhaseCompleted {
			count++
		}
	}
	return count
}

// countAllFailedTasks counts all failed tasks across all stages
func (r *WorkflowRunReconciler) countAllFailedTasks(ctx context.Context, workflowRun *kubetaskv1alpha1.WorkflowRun) int32 {
	taskList := &kubetaskv1alpha1.TaskList{}
	if err := r.List(ctx, taskList,
		client.InNamespace(workflowRun.Namespace),
		client.MatchingLabels{WorkflowRunLabelKey: workflowRun.Name}); err != nil {
		return 0
	}

	count := int32(0)
	for _, task := range taskList.Items {
		if task.Status.Phase == kubetaskv1alpha1.TaskPhaseFailed {
			count++
		}
	}
	return count
}

// SetupWithManager sets up the controller with the Manager
func (r *WorkflowRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubetaskv1alpha1.WorkflowRun{}).
		Owns(&kubetaskv1alpha1.Task{}).
		Complete(r)
}
