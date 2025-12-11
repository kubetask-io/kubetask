// Copyright Contributors to the KubeTask project

// Package controller implements Kubernetes controllers for KubeTask resources
package controller

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kubetaskv1alpha1 "github.com/kubetask-io/kubetask/api/v1alpha1"
)

const (
	// DefaultSuccessfulTasksHistoryLimit is the default number of successful tasks to keep
	DefaultSuccessfulTasksHistoryLimit int32 = 3

	// DefaultFailedTasksHistoryLimit is the default number of failed tasks to keep
	DefaultFailedTasksHistoryLimit int32 = 1

	// CronTaskLabelKey is the label key used to identify Tasks created by a CronTask
	CronTaskLabelKey = "kubetask.io/crontask"

	// ScheduledTimeAnnotation is the annotation key for the scheduled time
	ScheduledTimeAnnotation = "kubetask.io/scheduled-at"
)

// CronTaskReconciler reconciles a CronTask object
type CronTaskReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Clock  // for testing
}

// Clock interface for time operations, allows mocking in tests
type Clock interface {
	Now() time.Time
}

// realClock implements Clock using the real time
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// +kubebuilder:rbac:groups=kubetask.io,resources=crontasks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubetask.io,resources=crontasks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubetask.io,resources=crontasks/finalizers,verbs=update
// +kubebuilder:rbac:groups=kubetask.io,resources=tasks,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop
func (r *CronTaskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Use real clock if not set (for testing)
	if r.Clock == nil {
		r.Clock = realClock{}
	}

	// Get CronTask CR
	cronTask := &kubetaskv1alpha1.CronTask{}
	if err := r.Get(ctx, req.NamespacedName, cronTask); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch CronTask")
		return ctrl.Result{}, err
	}

	// Get all child Tasks for this CronTask
	childTasks, err := r.getChildTasks(ctx, cronTask)
	if err != nil {
		log.Error(err, "unable to list child Tasks")
		return ctrl.Result{}, err
	}

	// Categorize tasks by status
	var activeTasks, successfulTasks, failedTasks []*kubetaskv1alpha1.Task
	for i := range childTasks {
		task := &childTasks[i]
		switch task.Status.Phase {
		case kubetaskv1alpha1.TaskPhaseCompleted:
			successfulTasks = append(successfulTasks, task)
		case kubetaskv1alpha1.TaskPhaseFailed:
			failedTasks = append(failedTasks, task)
		case kubetaskv1alpha1.TaskPhaseRunning, kubetaskv1alpha1.TaskPhasePending:
			activeTasks = append(activeTasks, task)
		default:
			// New task without phase yet, consider it active
			activeTasks = append(activeTasks, task)
		}
	}

	// Update active task references in status
	activeRefs := make([]corev1.ObjectReference, len(activeTasks))
	for i, task := range activeTasks {
		activeRefs[i] = corev1.ObjectReference{
			APIVersion: task.APIVersion,
			Kind:       task.Kind,
			Name:       task.Name,
			Namespace:  task.Namespace,
			UID:        task.UID,
		}
	}
	cronTask.Status.Active = activeRefs

	// Clean up old tasks based on history limits
	if err := r.cleanupTasks(ctx, cronTask, successfulTasks, failedTasks); err != nil {
		log.Error(err, "unable to cleanup old Tasks")
		return ctrl.Result{}, err
	}

	// Check if suspended
	if cronTask.Spec.Suspend != nil && *cronTask.Spec.Suspend {
		log.V(1).Info("CronTask is suspended, skipping scheduling")
		if err := r.Status().Update(ctx, cronTask); err != nil {
			log.Error(err, "unable to update CronTask status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Parse cron schedule
	schedule, err := cron.ParseStandard(cronTask.Spec.Schedule)
	if err != nil {
		log.Error(err, "invalid cron schedule", "schedule", cronTask.Spec.Schedule)
		meta.SetStatusCondition(&cronTask.Status.Conditions, metav1.Condition{
			Type:    "Scheduled",
			Status:  metav1.ConditionFalse,
			Reason:  "InvalidSchedule",
			Message: fmt.Sprintf("Invalid cron schedule: %v", err),
		})
		if updateErr := r.Status().Update(ctx, cronTask); updateErr != nil {
			log.Error(updateErr, "unable to update CronTask status")
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil // Don't requeue, user needs to fix schedule
	}

	// Calculate next scheduled time and missed runs
	now := r.Now()
	scheduledTime, missedRuns := r.getNextSchedule(cronTask, now, schedule)

	if missedRuns > 0 {
		log.V(1).Info("missed scheduled runs", "count", missedRuns)
	}

	// Check if we need to create a new Task
	if scheduledTime != nil {
		// Handle concurrency policy
		if len(activeTasks) > 0 {
			switch cronTask.Spec.ConcurrencyPolicy {
			case kubetaskv1alpha1.ForbidConcurrent, "":
				// Skip this run
				log.V(1).Info("concurrency policy forbids concurrent runs, skipping", "active", len(activeTasks))
				// Update status and requeue for next schedule
				if err := r.Status().Update(ctx, cronTask); err != nil {
					log.Error(err, "unable to update CronTask status")
					return ctrl.Result{}, err
				}
				return r.requeueForNextSchedule(cronTask, now, schedule)
			case kubetaskv1alpha1.ReplaceConcurrent:
				// Delete all active tasks
				for _, task := range activeTasks {
					log.Info("deleting active task due to Replace policy", "task", task.Name)
					if err := r.Delete(ctx, task); err != nil && !errors.IsNotFound(err) {
						log.Error(err, "unable to delete active task", "task", task.Name)
						return ctrl.Result{}, err
					}
				}
				// Clear active references
				cronTask.Status.Active = nil
			case kubetaskv1alpha1.AllowConcurrent:
				// Allow, continue to create new task
			}
		}

		// Create new Task
		task, err := r.createTask(ctx, cronTask, *scheduledTime)
		if err != nil {
			log.Error(err, "unable to create Task")
			return ctrl.Result{}, err
		}

		log.Info("created Task", "task", task.Name, "scheduledTime", scheduledTime)

		// Update last schedule time
		cronTask.Status.LastScheduleTime = &metav1.Time{Time: *scheduledTime}

		// Update condition
		meta.SetStatusCondition(&cronTask.Status.Conditions, metav1.Condition{
			Type:    "Scheduled",
			Status:  metav1.ConditionTrue,
			Reason:  "TaskCreated",
			Message: fmt.Sprintf("Created Task %s", task.Name),
		})
	}

	// Update status
	if err := r.Status().Update(ctx, cronTask); err != nil {
		log.Error(err, "unable to update CronTask status")
		return ctrl.Result{}, err
	}

	// Requeue for next schedule
	return r.requeueForNextSchedule(cronTask, now, schedule)
}

// getChildTasks returns all Tasks owned by this CronTask
func (r *CronTaskReconciler) getChildTasks(ctx context.Context, cronTask *kubetaskv1alpha1.CronTask) ([]kubetaskv1alpha1.Task, error) {
	taskList := &kubetaskv1alpha1.TaskList{}
	if err := r.List(ctx, taskList, client.InNamespace(cronTask.Namespace), client.MatchingLabels{
		CronTaskLabelKey: cronTask.Name,
	}); err != nil {
		return nil, err
	}
	return taskList.Items, nil
}

// getNextSchedule calculates the next scheduled time and number of missed runs
func (r *CronTaskReconciler) getNextSchedule(cronTask *kubetaskv1alpha1.CronTask, now time.Time, schedule cron.Schedule) (*time.Time, int) {
	var lastScheduleTime time.Time
	if cronTask.Status.LastScheduleTime != nil {
		lastScheduleTime = cronTask.Status.LastScheduleTime.Time
	} else {
		// Use creation time as the starting point
		lastScheduleTime = cronTask.CreationTimestamp.Time
	}

	// If lastScheduleTime is in the future (clock skew), use creation time
	if lastScheduleTime.After(now) {
		lastScheduleTime = cronTask.CreationTimestamp.Time
	}

	// Find the next scheduled time after lastScheduleTime
	nextTime := schedule.Next(lastScheduleTime)

	// Count missed runs
	missedRuns := 0
	for nextTime.Before(now) {
		missedRuns++
		nextTime = schedule.Next(nextTime)
		// Safety limit to prevent infinite loops
		if missedRuns > 100 {
			break
		}
	}

	// Check if we should run now (if the last schedule time is before now)
	scheduledTime := schedule.Next(lastScheduleTime)
	if scheduledTime.Before(now) || scheduledTime.Equal(now) {
		return &scheduledTime, missedRuns
	}

	return nil, 0
}

// requeueForNextSchedule calculates when to requeue for the next scheduled run
func (r *CronTaskReconciler) requeueForNextSchedule(_ *kubetaskv1alpha1.CronTask, now time.Time, schedule cron.Schedule) (ctrl.Result, error) {
	nextRun := schedule.Next(now)
	requeueAfter := nextRun.Sub(now)

	// Add a small buffer to ensure we don't requeue too early
	if requeueAfter < time.Second {
		requeueAfter = time.Second
	}

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// createTask creates a new Task from the CronTask template
func (r *CronTaskReconciler) createTask(ctx context.Context, cronTask *kubetaskv1alpha1.CronTask, scheduledTime time.Time) (*kubetaskv1alpha1.Task, error) {
	// Generate unique task name using timestamp
	taskName := fmt.Sprintf("%s-%d", cronTask.Name, scheduledTime.Unix())

	// Create Task from template
	task := &kubetaskv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      taskName,
			Namespace: cronTask.Namespace,
			Labels: map[string]string{
				CronTaskLabelKey: cronTask.Name,
			},
			Annotations: map[string]string{
				ScheduledTimeAnnotation: scheduledTime.Format(time.RFC3339),
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: cronTask.APIVersion,
					Kind:       cronTask.Kind,
					Name:       cronTask.Name,
					UID:        cronTask.UID,
					Controller: boolPtr(true),
				},
			},
		},
		Spec: cronTask.Spec.TaskTemplate.Spec,
	}

	// Merge labels from template
	for k, v := range cronTask.Spec.TaskTemplate.Labels {
		task.Labels[k] = v
	}

	// Merge annotations from template
	for k, v := range cronTask.Spec.TaskTemplate.Annotations {
		task.Annotations[k] = v
	}

	if err := r.Create(ctx, task); err != nil {
		return nil, err
	}

	return task, nil
}

// cleanupTasks removes old tasks based on history limits
func (r *CronTaskReconciler) cleanupTasks(ctx context.Context, cronTask *kubetaskv1alpha1.CronTask, successfulTasks, failedTasks []*kubetaskv1alpha1.Task) error {
	log := log.FromContext(ctx)

	successLimit := DefaultSuccessfulTasksHistoryLimit
	if cronTask.Spec.SuccessfulTasksHistoryLimit != nil {
		successLimit = *cronTask.Spec.SuccessfulTasksHistoryLimit
	}

	failedLimit := DefaultFailedTasksHistoryLimit
	if cronTask.Spec.FailedTasksHistoryLimit != nil {
		failedLimit = *cronTask.Spec.FailedTasksHistoryLimit
	}

	// Sort tasks by creation time (oldest first)
	sortByCreationTime := func(tasks []*kubetaskv1alpha1.Task) {
		sort.Slice(tasks, func(i, j int) bool {
			return tasks[i].CreationTimestamp.Before(&tasks[j].CreationTimestamp)
		})
	}

	// Cleanup successful tasks
	sortByCreationTime(successfulTasks)
	for i := 0; i < len(successfulTasks)-int(successLimit); i++ {
		task := successfulTasks[i]
		log.V(1).Info("deleting old successful task", "task", task.Name)
		if err := r.Delete(ctx, task); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	// Cleanup failed tasks
	sortByCreationTime(failedTasks)
	for i := 0; i < len(failedTasks)-int(failedLimit); i++ {
		task := failedTasks[i]
		log.V(1).Info("deleting old failed task", "task", task.Name)
		if err := r.Delete(ctx, task); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *CronTaskReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Index Tasks by the CronTask label for efficient lookup
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &kubetaskv1alpha1.Task{}, ".metadata.controller", func(rawObj client.Object) []string {
		task := rawObj.(*kubetaskv1alpha1.Task)
		owner := metav1.GetControllerOf(task)
		if owner == nil {
			return nil
		}
		if owner.APIVersion != kubetaskv1alpha1.GroupVersion.String() || owner.Kind != "CronTask" {
			return nil
		}
		return []string{types.NamespacedName{Namespace: task.Namespace, Name: owner.Name}.String()}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&kubetaskv1alpha1.CronTask{}).
		Owns(&kubetaskv1alpha1.Task{}).
		Complete(r)
}
