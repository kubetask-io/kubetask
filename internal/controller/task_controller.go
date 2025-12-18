// Copyright Contributors to the KubeTask project

// Package controller implements Kubernetes controllers for KubeTask resources
package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
)

const (
	// DefaultAgentImage is the default agent container image
	DefaultAgentImage = "quay.io/kubetask/kubetask-agent-gemini:latest"

	// ContextConfigMapSuffix is the suffix for ConfigMap names created for context
	ContextConfigMapSuffix = "-context"

	// DefaultTTLSecondsAfterFinished is the default TTL for completed/failed tasks (7 days)
	DefaultTTLSecondsAfterFinished int32 = 604800

	// DefaultSessionDuration is the default session duration for human-in-the-loop (1 hour)
	DefaultSessionDuration = time.Hour

	// EnvHumanInTheLoopDuration is the environment variable name for session duration in seconds
	EnvHumanInTheLoopDuration = "KUBETASK_SESSION_DURATION_SECONDS"

	// AgentLabelKey is the label key used to identify which Agent a Task uses
	AgentLabelKey = "kubetask.io/agent"

	// DefaultQueuedRequeueDelay is the default delay for requeuing queued Tasks
	DefaultQueuedRequeueDelay = 10 * time.Second

	// AnnotationStop is the annotation key for user-initiated task stop
	AnnotationStop = "kubetask.io/stop"

	// AnnotationHumanInTheLoop indicates that humanInTheLoop is enabled for the task
	AnnotationHumanInTheLoop = "kubetask.io/human-in-the-loop"

	// RuntimeSystemPrompt is the system prompt injected when Runtime context is enabled.
	// It provides KubeTask platform awareness to the agent.
	RuntimeSystemPrompt = `## KubeTask Runtime Context

You are running as an AI agent inside a Kubernetes Pod, managed by KubeTask.

### Environment Variables
- TASK_NAME: Name of the current Task CR
- TASK_NAMESPACE: Namespace of the current Task CR
- WORKSPACE_DIR: Working directory where task.md and context files are mounted

### Getting More Information
To get full Task specification:
  kubectl get task ${TASK_NAME} -n ${TASK_NAMESPACE} -o yaml

To get Task status:
  kubectl get task ${TASK_NAME} -n ${TASK_NAMESPACE} -o jsonpath='{.status}'

To list related resources:
  kubectl get tasks,workflows,workflowruns -n ${TASK_NAMESPACE}

### File Structure
- ${WORKSPACE_DIR}/task.md: Your task instructions (this file)
- Additional contexts may be mounted as separate files or appended below

### KubeTask Concepts
- Task: Single AI task execution (what you're running now)
- Agent: Configuration for how tasks are executed (image, credentials, etc.)
- Workflow: Multi-stage task orchestration template
- WorkflowRun: Execution instance of a Workflow
- Context: Reusable content that can be shared across Tasks
`
)

// TaskReconciler reconciles a Task object
type TaskReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kubetask.io,resources=tasks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubetask.io,resources=tasks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubetask.io,resources=tasks/finalizers,verbs=update
// +kubebuilder:rbac:groups=kubetask.io,resources=agents,verbs=get;list;watch
// +kubebuilder:rbac:groups=kubetask.io,resources=contexts,verbs=get;list;watch
// +kubebuilder:rbac:groups=kubetask.io,resources=kubetaskconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop
func (r *TaskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Get Task CR
	task := &kubetaskv1alpha1.Task{}
	if err := r.Get(ctx, req.NamespacedName, task); err != nil {
		if errors.IsNotFound(err) {
			// Task deleted, nothing to do
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch Task")
		return ctrl.Result{}, err
	}

	// If new, initialize status and create Job
	if task.Status.Phase == "" {
		return r.initializeTask(ctx, task)
	}

	// If queued, check if capacity is available
	if task.Status.Phase == kubetaskv1alpha1.TaskPhaseQueued {
		return r.handleQueuedTask(ctx, task)
	}

	// If completed/failed, check TTL for cleanup
	if task.Status.Phase == kubetaskv1alpha1.TaskPhaseCompleted ||
		task.Status.Phase == kubetaskv1alpha1.TaskPhaseFailed {
		return r.handleTaskCleanup(ctx, task)
	}

	// Check for user-initiated stop (only for Running tasks)
	if task.Status.Phase == kubetaskv1alpha1.TaskPhaseRunning {
		if task.Annotations != nil && task.Annotations[AnnotationStop] == "true" {
			return r.handleStop(ctx, task)
		}
	}

	// Update task status from Job status
	if err := r.updateTaskStatusFromJob(ctx, task); err != nil {
		log.Error(err, "unable to update task status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// initializeTask initializes a new Task and creates its Job
func (r *TaskReconciler) initializeTask(ctx context.Context, task *kubetaskv1alpha1.Task) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Get agent configuration with name
	agentConfig, agentName, err := r.getAgentConfigWithName(ctx, task)
	if err != nil {
		log.Error(err, "unable to get Agent")
		// Update task status to Failed
		task.Status.ObservedGeneration = task.Generation
		task.Status.Phase = kubetaskv1alpha1.TaskPhaseFailed
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "AgentError",
			Message: err.Error(),
		})
		if updateErr := r.Status().Update(ctx, task); updateErr != nil {
			log.Error(updateErr, "unable to update Task status")
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil // Don't requeue, user needs to fix Agent
	}

	// Validate humanInTheLoop configuration: Duration and Command are mutually exclusive
	if agentConfig.humanInTheLoop != nil && agentConfig.humanInTheLoop.Enabled {
		if agentConfig.humanInTheLoop.Duration != nil && len(agentConfig.humanInTheLoop.Command) > 0 {
			err := fmt.Errorf("humanInTheLoop.duration and humanInTheLoop.command are mutually exclusive")
			log.Error(err, "invalid Agent configuration")
			task.Status.ObservedGeneration = task.Generation
			task.Status.Phase = kubetaskv1alpha1.TaskPhaseFailed
			meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  "AgentConfigError",
				Message: err.Error(),
			})
			if updateErr := r.Status().Update(ctx, task); updateErr != nil {
				log.Error(updateErr, "unable to update Task status")
				return ctrl.Result{}, updateErr
			}
			return ctrl.Result{}, nil // Don't requeue, user needs to fix Agent
		}
	}

	// Add agent label and humanInTheLoop annotation to Task
	needsUpdate := false
	if task.Labels == nil {
		task.Labels = make(map[string]string)
	}
	if task.Labels[AgentLabelKey] != agentName {
		task.Labels[AgentLabelKey] = agentName
		needsUpdate = true
	}

	// Add humanInTheLoop annotation if enabled on Agent
	if agentConfig.humanInTheLoop != nil && agentConfig.humanInTheLoop.Enabled {
		if task.Annotations == nil {
			task.Annotations = make(map[string]string)
		}
		if task.Annotations[AnnotationHumanInTheLoop] != "true" {
			task.Annotations[AnnotationHumanInTheLoop] = "true"
			needsUpdate = true
		}
	}

	if needsUpdate {
		if err := r.Update(ctx, task); err != nil {
			log.Error(err, "unable to update Task")
			return ctrl.Result{}, err
		}
		// Requeue to continue with updated task
		return ctrl.Result{Requeue: true}, nil
	}

	// Check agent capacity if MaxConcurrentTasks is set
	if agentConfig.maxConcurrentTasks != nil && *agentConfig.maxConcurrentTasks > 0 {
		hasCapacity, err := r.checkAgentCapacity(ctx, task.Namespace, agentName, *agentConfig.maxConcurrentTasks)
		if err != nil {
			log.Error(err, "unable to check agent capacity")
			return ctrl.Result{}, err
		}

		if !hasCapacity {
			// Agent is at capacity, queue the task
			log.Info("agent at capacity, queueing task", "agent", agentName, "maxConcurrent", *agentConfig.maxConcurrentTasks)

			task.Status.ObservedGeneration = task.Generation
			task.Status.Phase = kubetaskv1alpha1.TaskPhaseQueued

			meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
				Type:    "Queued",
				Status:  metav1.ConditionTrue,
				Reason:  "AgentAtCapacity",
				Message: fmt.Sprintf("Waiting for agent %q capacity (max: %d)", agentName, *agentConfig.maxConcurrentTasks),
			})

			if err := r.Status().Update(ctx, task); err != nil {
				log.Error(err, "unable to update Task status")
				return ctrl.Result{}, err
			}

			// Requeue with delay
			return ctrl.Result{RequeueAfter: DefaultQueuedRequeueDelay}, nil
		}
	}

	// Generate Job name
	jobName := fmt.Sprintf("%s-job", task.Name)

	// Check if Job already exists
	existingJob := &batchv1.Job{}
	jobKey := types.NamespacedName{Name: jobName, Namespace: task.Namespace}
	if err := r.Get(ctx, jobKey, existingJob); err == nil {
		// Job already exists, update status
		task.Status.ObservedGeneration = task.Generation
		task.Status.JobName = jobName
		task.Status.Phase = kubetaskv1alpha1.TaskPhaseRunning
		now := metav1.Now()
		task.Status.StartTime = &now
		return ctrl.Result{}, r.Status().Update(ctx, task)
	}

	// Process all contexts using priority-based resolution
	// Priority (lowest to highest):
	//   1. Agent.contexts (Agent-level Context CRD references)
	//   2. Task.contexts (Task-specific Context CRD references)
	//   3. Task.description (highest, becomes start of ${WORKSPACE_DIR}/task.md)
	contextConfigMap, fileMounts, dirMounts, gitMounts, err := r.processAllContexts(ctx, task, agentConfig)
	if err != nil {
		log.Error(err, "unable to process contexts")
		// Update task status to Failed - context errors are user configuration issues
		task.Status.ObservedGeneration = task.Generation
		task.Status.Phase = kubetaskv1alpha1.TaskPhaseFailed
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "ContextError",
			Message: err.Error(),
		})
		if updateErr := r.Status().Update(ctx, task); updateErr != nil {
			log.Error(updateErr, "unable to update Task status")
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil // Don't requeue, user needs to fix context configuration
	}

	// Create ConfigMap if there's aggregated content
	if contextConfigMap != nil {
		if err := r.Create(ctx, contextConfigMap); err != nil {
			if !errors.IsAlreadyExists(err) {
				log.Error(err, "unable to create context ConfigMap")
				return ctrl.Result{}, err
			}
		}
	}

	// Create Job with agent configuration and context mounts
	job := buildJob(task, jobName, agentConfig, contextConfigMap, fileMounts, dirMounts, gitMounts)

	if err := r.Create(ctx, job); err != nil {
		log.Error(err, "unable to create Job", "job", jobName)
		return ctrl.Result{}, err
	}

	// Update status
	task.Status.ObservedGeneration = task.Generation
	task.Status.JobName = jobName
	task.Status.Phase = kubetaskv1alpha1.TaskPhaseRunning
	now := metav1.Now()
	task.Status.StartTime = &now

	if err := r.Status().Update(ctx, task); err != nil {
		log.Error(err, "unable to update Task status")
		return ctrl.Result{}, err
	}

	log.Info("initialized Task", "job", jobName, "image", agentConfig.agentImage)
	return ctrl.Result{}, nil
}

// updateTaskStatusFromJob syncs task status from Job status
func (r *TaskReconciler) updateTaskStatusFromJob(ctx context.Context, task *kubetaskv1alpha1.Task) error {
	log := log.FromContext(ctx)

	if task.Status.JobName == "" {
		return nil
	}

	// Get Job status
	job := &batchv1.Job{}
	jobKey := types.NamespacedName{Name: task.Status.JobName, Namespace: task.Namespace}
	if err := r.Get(ctx, jobKey, job); err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "Job not found", "job", task.Status.JobName)
			return nil
		}
		return err
	}

	// Check Job completion
	if job.Status.Succeeded > 0 {
		task.Status.ObservedGeneration = task.Generation
		task.Status.Phase = kubetaskv1alpha1.TaskPhaseCompleted
		now := metav1.Now()
		task.Status.CompletionTime = &now
		log.Info("task completed", "job", task.Status.JobName)
		return r.Status().Update(ctx, task)
	} else if job.Status.Failed > 0 {
		task.Status.ObservedGeneration = task.Generation
		task.Status.Phase = kubetaskv1alpha1.TaskPhaseFailed
		now := metav1.Now()
		task.Status.CompletionTime = &now
		log.Info("task failed", "job", task.Status.JobName)
		return r.Status().Update(ctx, task)
	}

	return nil
}

// handleTaskCleanup checks if a completed/failed task should be deleted based on TTL
func (r *TaskReconciler) handleTaskCleanup(ctx context.Context, task *kubetaskv1alpha1.Task) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Get TTL configuration
	ttlSeconds := r.getTTLSecondsAfterFinished(ctx, task.Namespace)

	// TTL of 0 means no automatic cleanup
	if ttlSeconds == 0 {
		return ctrl.Result{}, nil
	}

	// Check if task has completion time
	if task.Status.CompletionTime == nil {
		return ctrl.Result{}, nil
	}

	// Calculate time since completion
	completionTime := task.Status.CompletionTime.Time
	ttlDuration := time.Duration(ttlSeconds) * time.Second
	expirationTime := completionTime.Add(ttlDuration)
	now := time.Now()

	if now.After(expirationTime) {
		// Task has expired, delete it
		log.Info("deleting expired task", "task", task.Name, "completedAt", completionTime, "ttl", ttlSeconds)
		if err := r.Delete(ctx, task); err != nil {
			if !errors.IsNotFound(err) {
				log.Error(err, "unable to delete expired task")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Task not yet expired, requeue to check again at expiration time
	requeueAfter := expirationTime.Sub(now)
	log.V(1).Info("task not yet expired, requeueing", "task", task.Name, "requeueAfter", requeueAfter)
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// getTTLSecondsAfterFinished retrieves the TTL configuration from KubeTaskConfig.
// It looks for config in the following order:
// 1. KubeTaskConfig named "default" in the task's namespace
// 2. Built-in default (7 days)
func (r *TaskReconciler) getTTLSecondsAfterFinished(ctx context.Context, namespace string) int32 {
	log := log.FromContext(ctx)

	// Try to get KubeTaskConfig from the task's namespace
	config := &kubetaskv1alpha1.KubeTaskConfig{}
	configKey := types.NamespacedName{Name: "default", Namespace: namespace}

	if err := r.Get(ctx, configKey, config); err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "unable to get KubeTaskConfig, using default TTL")
		}
		// Config not found, use built-in default
		return DefaultTTLSecondsAfterFinished
	}

	// Config found, extract TTL
	if config.Spec.TaskLifecycle != nil && config.Spec.TaskLifecycle.TTLSecondsAfterFinished != nil {
		return *config.Spec.TaskLifecycle.TTLSecondsAfterFinished
	}

	return DefaultTTLSecondsAfterFinished
}

// SetupWithManager sets up the controller with the Manager
func (r *TaskReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubetaskv1alpha1.Task{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}

// getAgentConfigWithName retrieves the agent configuration and returns the agent name.
// This is useful when we need to track which agent a task is using.
func (r *TaskReconciler) getAgentConfigWithName(ctx context.Context, task *kubetaskv1alpha1.Task) (agentConfig, string, error) {
	log := log.FromContext(ctx)

	// Determine which Agent to use
	agentName := "default"
	if task.Spec.AgentRef != "" {
		agentName = task.Spec.AgentRef
	}

	// Get Agent
	agent := &kubetaskv1alpha1.Agent{}
	agentKey := types.NamespacedName{
		Name:      agentName,
		Namespace: task.Namespace,
	}

	if err := r.Get(ctx, agentKey, agent); err != nil {
		log.Error(err, "unable to get Agent", "agent", agentName)
		return agentConfig{}, "", fmt.Errorf("agent %q not found in namespace %q: %w", agentName, task.Namespace, err)
	}

	// Get agent image (optional, has default)
	agentImage := DefaultAgentImage
	if agent.Spec.AgentImage != "" {
		agentImage = agent.Spec.AgentImage
	}

	// Get workspace directory (required)
	workspaceDir := agent.Spec.WorkspaceDir

	// ServiceAccountName is required
	if agent.Spec.ServiceAccountName == "" {
		return agentConfig{}, "", fmt.Errorf("agent %q is missing required field serviceAccountName", agentName)
	}

	return agentConfig{
		agentImage:         agentImage,
		command:            agent.Spec.Command,
		workspaceDir:       workspaceDir,
		contexts:           agent.Spec.Contexts,
		credentials:        agent.Spec.Credentials,
		podSpec:            agent.Spec.PodSpec,
		serviceAccountName: agent.Spec.ServiceAccountName,
		maxConcurrentTasks: agent.Spec.MaxConcurrentTasks,
		humanInTheLoop:     agent.Spec.HumanInTheLoop,
	}, agentName, nil
}

// processAllContexts processes all contexts from Agent and Task, resolving Context CRs
// and returning the ConfigMap, file mounts, directory mounts, and git mounts for the Job.
//
// Content order in task.md (top to bottom):
//  1. Task.description (appears first in task.md)
//  2. Agent.contexts (Agent-level Context CRD references)
//  3. Task.contexts (Task-specific Context CRD references, appears last)
func (r *TaskReconciler) processAllContexts(ctx context.Context, task *kubetaskv1alpha1.Task, cfg agentConfig) (*corev1.ConfigMap, []fileMount, []dirMount, []gitMount, error) {
	var resolved []resolvedContext
	var dirMounts []dirMount
	var gitMounts []gitMount

	// 1. Resolve Agent.contexts (appears after description in task.md)
	for i, src := range cfg.contexts {
		rc, dm, gm, err := r.resolveContextSource(ctx, src, task.Namespace, cfg.workspaceDir)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("failed to resolve Agent context[%d]: %w", i, err)
		}
		switch {
		case dm != nil:
			dirMounts = append(dirMounts, *dm)
		case gm != nil:
			gitMounts = append(gitMounts, *gm)
		case rc != nil:
			resolved = append(resolved, *rc)
		}
	}

	// 2. Resolve Task.contexts (appears last in task.md)
	for i, src := range task.Spec.Contexts {
		rc, dm, gm, err := r.resolveContextSource(ctx, src, task.Namespace, cfg.workspaceDir)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("failed to resolve Task context[%d]: %w", i, err)
		}
		switch {
		case dm != nil:
			dirMounts = append(dirMounts, *dm)
		case gm != nil:
			gitMounts = append(gitMounts, *gm)
		case rc != nil:
			resolved = append(resolved, *rc)
		}
	}

	// 3. Handle Task.description (highest priority, becomes ${WORKSPACE_DIR}/task.md)
	var taskDescription string
	if task.Spec.Description != nil && *task.Spec.Description != "" {
		taskDescription = *task.Spec.Description
	}

	// Build the final content
	// - Separate contexts with mountPath (independent files)
	// - Contexts without mountPath are appended to task.md with XML tags
	configMapData := make(map[string]string)
	var fileMounts []fileMount

	// Build task.md content: description + contexts without mountPath
	var taskMdParts []string
	if taskDescription != "" {
		taskMdParts = append(taskMdParts, taskDescription)
	}

	for _, rc := range resolved {
		if rc.mountPath != "" {
			// Context has explicit mountPath - create separate file
			configMapKey := sanitizeConfigMapKey(rc.mountPath)
			configMapData[configMapKey] = rc.content
			fileMounts = append(fileMounts, fileMount{filePath: rc.mountPath})
		} else {
			// No mountPath - append to task.md with XML tags
			xmlTag := fmt.Sprintf("<context name=%q namespace=%q type=%q>\n%s\n</context>",
				rc.name, rc.namespace, rc.ctxType, rc.content)
			taskMdParts = append(taskMdParts, xmlTag)
		}
	}

	// Create task.md if there's any content
	// Mount at the configured workspace directory
	taskMdPath := cfg.workspaceDir + "/task.md"
	if len(taskMdParts) > 0 {
		taskMdContent := strings.Join(taskMdParts, "\n\n")
		configMapData["workspace-task.md"] = taskMdContent
		fileMounts = append(fileMounts, fileMount{filePath: taskMdPath})
	}

	// Create ConfigMap if there's any content
	var configMap *corev1.ConfigMap
	if len(configMapData) > 0 {
		configMapName := task.Name + ContextConfigMapSuffix
		configMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: task.Namespace,
				Labels: map[string]string{
					"app":              "kubetask",
					"kubetask.io/task": task.Name,
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: task.APIVersion,
						Kind:       task.Kind,
						Name:       task.Name,
						UID:        task.UID,
						Controller: boolPtr(true),
					},
				},
			},
			Data: configMapData,
		}
	}

	// Validate mount path conflicts
	// Multiple contexts mounting to the same path would silently overwrite each other,
	// so we detect and report conflicts explicitly.
	if err := validateMountPathConflicts(fileMounts, dirMounts, gitMounts); err != nil {
		return nil, nil, nil, nil, err
	}

	return configMap, fileMounts, dirMounts, gitMounts, nil
}

// validateMountPathConflicts checks for duplicate mount paths across all mount types.
// Returns an error if any two mounts target the same path.
func validateMountPathConflicts(fileMounts []fileMount, dirMounts []dirMount, gitMounts []gitMount) error {
	mountPaths := make(map[string]string) // path -> source description

	for _, fm := range fileMounts {
		if existing, ok := mountPaths[fm.filePath]; ok {
			return fmt.Errorf("mount path conflict: %q is used by both %s and a file mount", fm.filePath, existing)
		}
		mountPaths[fm.filePath] = "file mount"
	}

	for _, dm := range dirMounts {
		if existing, ok := mountPaths[dm.dirPath]; ok {
			return fmt.Errorf("mount path conflict: %q is used by both %s and directory mount (ConfigMap: %s)", dm.dirPath, existing, dm.configMapName)
		}
		mountPaths[dm.dirPath] = fmt.Sprintf("directory mount (ConfigMap: %s)", dm.configMapName)
	}

	for _, gm := range gitMounts {
		if existing, ok := mountPaths[gm.mountPath]; ok {
			return fmt.Errorf("mount path conflict: %q is used by both %s and git mount (%s)", gm.mountPath, existing, gm.contextName)
		}
		mountPaths[gm.mountPath] = fmt.Sprintf("git mount (%s)", gm.contextName)
	}

	return nil
}

// resolveContextSource resolves a ContextSource (either a reference to a Context CR or an inline definition)
func (r *TaskReconciler) resolveContextSource(ctx context.Context, src kubetaskv1alpha1.ContextSource, defaultNS, workspaceDir string) (*resolvedContext, *dirMount, *gitMount, error) {
	// Handle reference to Context CR
	if src.Ref != nil {
		return r.resolveContextRef(ctx, src.Ref, defaultNS, workspaceDir)
	}

	// Handle inline context
	if src.Inline != nil {
		return r.resolveContextItem(ctx, src.Inline, defaultNS, workspaceDir)
	}

	// Neither Ref nor Inline specified - this is a validation error
	return nil, nil, nil, fmt.Errorf("ContextSource must have either Ref or Inline specified")
}

// resolveContextRef resolves a ContextRef reference to a Context CR
func (r *TaskReconciler) resolveContextRef(ctx context.Context, ref *kubetaskv1alpha1.ContextRef, defaultNS, workspaceDir string) (*resolvedContext, *dirMount, *gitMount, error) {
	namespace := ref.Namespace
	if namespace == "" {
		namespace = defaultNS
	}

	// Fetch the Context CR
	contextCR := &kubetaskv1alpha1.Context{}
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, contextCR); err != nil {
		return nil, nil, nil, fmt.Errorf("context %q not found in namespace %q: %w", ref.Name, namespace, err)
	}

	// Resolve mountPath: relative paths are prefixed with workspaceDir
	resolvedPath := resolveMountPath(ref.MountPath, workspaceDir)

	// Resolve content based on context type
	content, dm, gm, err := r.resolveContextSpec(ctx, namespace, ref.Name, workspaceDir, &contextCR.Spec, resolvedPath)
	if err != nil {
		return nil, nil, nil, err
	}

	if dm != nil {
		return nil, dm, nil, nil
	}

	if gm != nil {
		return nil, nil, gm, nil
	}

	return &resolvedContext{
		name:      ref.Name,
		namespace: namespace,
		ctxType:   string(contextCR.Spec.Type),
		content:   content,
		mountPath: resolvedPath,
	}, nil, nil, nil
}

// resolveContextItem resolves an inline ContextItem
func (r *TaskReconciler) resolveContextItem(ctx context.Context, item *kubetaskv1alpha1.ContextItem, defaultNS, workspaceDir string) (*resolvedContext, *dirMount, *gitMount, error) {
	// Validate: inline Git context requires mountPath to be specified
	// Unlike Context CRDs which have a name for automatic path generation (git-<name>),
	// inline contexts use a hardcoded "inline" name which would cause conflicts
	// if multiple inline Git contexts are used without explicit mountPath.
	if item.Type == kubetaskv1alpha1.ContextTypeGit && item.MountPath == "" {
		return nil, nil, nil, fmt.Errorf("inline Git context requires mountPath to be specified; use a Context CRD for automatic path generation")
	}

	// Create a temporary ContextSpec from the ContextItem
	spec := &kubetaskv1alpha1.ContextSpec{
		Type:      item.Type,
		Text:      item.Text,
		ConfigMap: item.ConfigMap,
		Git:       item.Git,
		Runtime:   item.Runtime,
	}

	// Use a generated name for inline contexts
	// For Runtime context, use "runtime" as a more descriptive name
	name := "inline"
	if item.Type == kubetaskv1alpha1.ContextTypeRuntime {
		name = "runtime"
	}

	// Resolve mountPath: relative paths are prefixed with workspaceDir
	// Note: For Runtime context, mountPath is ignored - content is always appended to task.md
	resolvedPath := resolveMountPath(item.MountPath, workspaceDir)
	if item.Type == kubetaskv1alpha1.ContextTypeRuntime {
		resolvedPath = "" // Force empty to ensure content is appended to task.md
	}

	// Resolve content based on context type
	content, dm, gm, err := r.resolveContextSpec(ctx, defaultNS, name, workspaceDir, spec, resolvedPath)
	if err != nil {
		return nil, nil, nil, err
	}

	if dm != nil {
		return nil, dm, nil, nil
	}

	if gm != nil {
		return nil, nil, gm, nil
	}

	return &resolvedContext{
		name:      name,
		namespace: defaultNS,
		ctxType:   string(item.Type),
		content:   content,
		mountPath: resolvedPath,
	}, nil, nil, nil
}

// resolveMountPath converts relative paths to absolute paths based on workspaceDir.
// Paths starting with "/" are treated as absolute and returned as-is.
// Paths NOT starting with "/" are treated as relative and prefixed with workspaceDir.
// This follows Tekton conventions for workspace path resolution.
func resolveMountPath(mountPath, workspaceDir string) string {
	if mountPath == "" {
		return ""
	}
	if strings.HasPrefix(mountPath, "/") {
		return mountPath
	}
	return workspaceDir + "/" + mountPath
}

// resolveContextSpec resolves content from a ContextSpec (used by Context CRD)
// Returns: content string, dirMount pointer, gitMount pointer, error
func (r *TaskReconciler) resolveContextSpec(ctx context.Context, namespace, name, workspaceDir string, spec *kubetaskv1alpha1.ContextSpec, mountPath string) (string, *dirMount, *gitMount, error) {
	switch spec.Type {
	case kubetaskv1alpha1.ContextTypeText:
		if spec.Text == "" {
			return "", nil, nil, nil
		}
		return spec.Text, nil, nil, nil

	case kubetaskv1alpha1.ContextTypeConfigMap:
		if spec.ConfigMap == nil {
			return "", nil, nil, nil
		}
		cm := spec.ConfigMap

		// If Key is specified, return the content
		if cm.Key != "" {
			content, err := r.getConfigMapKey(ctx, namespace, cm.Name, cm.Key, cm.Optional)
			return content, nil, nil, err
		}

		// If Key is not specified but mountPath is, return a directory mount
		if mountPath != "" {
			optional := false
			if cm.Optional != nil {
				optional = *cm.Optional
			}
			return "", &dirMount{
				dirPath:       mountPath,
				configMapName: cm.Name,
				optional:      optional,
			}, nil, nil
		}

		// If Key is not specified and mountPath is empty, aggregate all keys to task.md
		content, err := r.getConfigMapAllKeys(ctx, namespace, cm.Name, cm.Optional)
		return content, nil, nil, err

	case kubetaskv1alpha1.ContextTypeGit:
		if spec.Git == nil {
			return "", nil, nil, nil
		}
		git := spec.Git

		// Determine mount path: use specified path or default to ${WORKSPACE_DIR}/git-<context-name>/
		resolvedMountPath := mountPath
		if resolvedMountPath == "" {
			resolvedMountPath = workspaceDir + "/git-" + name
		}

		// Determine clone depth: default to 1 (shallow clone)
		depth := 1
		if git.Depth != nil && *git.Depth > 0 {
			depth = *git.Depth
		}

		// Determine ref: default to HEAD
		ref := git.Ref
		if ref == "" {
			ref = "HEAD"
		}

		// Get secret name if specified
		secretName := ""
		if git.SecretRef != nil {
			secretName = git.SecretRef.Name
		}

		return "", nil, &gitMount{
			contextName: name,
			repository:  git.Repository,
			ref:         ref,
			repoPath:    git.Path,
			mountPath:   resolvedMountPath,
			depth:       depth,
			secretName:  secretName,
		}, nil

	case kubetaskv1alpha1.ContextTypeRuntime:
		// Runtime context returns the hardcoded system prompt
		// MountPath is ignored for Runtime context - content is always appended to task.md
		return RuntimeSystemPrompt, nil, nil, nil

	default:
		return "", nil, nil, fmt.Errorf("unknown context type: %s", spec.Type)
	}
}

// getConfigMapKey retrieves a specific key from a ConfigMap
func (r *TaskReconciler) getConfigMapKey(ctx context.Context, namespace, name, key string, optional *bool) (string, error) {
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, cm); err != nil {
		if optional != nil && *optional {
			return "", nil
		}
		return "", err
	}
	if content, ok := cm.Data[key]; ok {
		return content, nil
	}
	if optional != nil && *optional {
		return "", nil
	}
	return "", fmt.Errorf("key %s not found in ConfigMap %s", key, name)
}

// getConfigMapAllKeys retrieves all keys from a ConfigMap and formats them for aggregation
func (r *TaskReconciler) getConfigMapAllKeys(ctx context.Context, namespace, name string, optional *bool) (string, error) {
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, cm); err != nil {
		if optional != nil && *optional {
			return "", nil
		}
		return "", err
	}

	if len(cm.Data) == 0 {
		return "", nil
	}

	// Sort keys for deterministic output
	keys := make([]string, 0, len(cm.Data))
	for k := range cm.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("<file name=%q>\n%s\n</file>", key, cm.Data[key]))
	}
	return strings.Join(parts, "\n"), nil
}

// checkAgentCapacity checks if the agent has capacity for a new task.
// Returns true if capacity is available, false if at limit.
func (r *TaskReconciler) checkAgentCapacity(ctx context.Context, namespace, agentName string, maxConcurrent int32) (bool, error) {
	log := log.FromContext(ctx)

	// List all Tasks for this Agent using label selector
	taskList := &kubetaskv1alpha1.TaskList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels{AgentLabelKey: agentName},
	}

	if err := r.List(ctx, taskList, listOpts...); err != nil {
		return false, err
	}

	// Count running tasks (those with Jobs created and in progress)
	runningCount := int32(0)
	for i := range taskList.Items {
		task := &taskList.Items[i]
		// Count tasks that are Running
		if task.Status.Phase == kubetaskv1alpha1.TaskPhaseRunning {
			runningCount++
		}
	}

	log.V(1).Info("agent capacity check", "agent", agentName, "running", runningCount, "max", maxConcurrent)

	return runningCount < maxConcurrent, nil
}

// handleQueuedTask checks if a queued task can now be started
func (r *TaskReconciler) handleQueuedTask(ctx context.Context, task *kubetaskv1alpha1.Task) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Get agent configuration with name
	agentConfig, agentName, err := r.getAgentConfigWithName(ctx, task)
	if err != nil {
		log.Error(err, "unable to get Agent for queued task")
		// Agent might be deleted, fail the task
		task.Status.Phase = kubetaskv1alpha1.TaskPhaseFailed
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "AgentError",
			Message: err.Error(),
		})
		if updateErr := r.Status().Update(ctx, task); updateErr != nil {
			log.Error(updateErr, "unable to update Task status")
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil
	}

	// Check if agent still has MaxConcurrentTasks set
	if agentConfig.maxConcurrentTasks == nil || *agentConfig.maxConcurrentTasks <= 0 {
		// Limit removed, proceed to initialize
		log.Info("agent capacity limit removed, proceeding with task", "agent", agentName)
		task.Status.Phase = ""
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:    "Queued",
			Status:  metav1.ConditionFalse,
			Reason:  "CapacityAvailable",
			Message: fmt.Sprintf("Agent %q capacity limit removed", agentName),
		})
		if err := r.Status().Update(ctx, task); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Check if capacity is now available
	hasCapacity, err := r.checkAgentCapacity(ctx, task.Namespace, agentName, *agentConfig.maxConcurrentTasks)
	if err != nil {
		log.Error(err, "unable to check agent capacity")
		return ctrl.Result{}, err
	}

	if !hasCapacity {
		// Still at capacity, requeue
		log.V(1).Info("agent still at capacity, remaining queued", "agent", agentName)
		return ctrl.Result{RequeueAfter: DefaultQueuedRequeueDelay}, nil
	}

	// Capacity available, transition to empty phase to trigger initializeTask
	log.Info("agent capacity available, transitioning to initialize", "agent", agentName)
	task.Status.Phase = ""
	meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
		Type:    "Queued",
		Status:  metav1.ConditionFalse,
		Reason:  "CapacityAvailable",
		Message: fmt.Sprintf("Agent %q capacity now available", agentName),
	})

	if err := r.Status().Update(ctx, task); err != nil {
		log.Error(err, "unable to update queued task status")
		return ctrl.Result{}, err
	}

	// Requeue immediately to trigger initializeTask
	return ctrl.Result{Requeue: true}, nil
}

// handleStop handles user-initiated task stop via annotation.
// It suspends the Job which triggers graceful termination of running Pods via SIGTERM.
// The Job and Pod are preserved (not deleted) so logs remain accessible.
func (r *TaskReconciler) handleStop(ctx context.Context, task *kubetaskv1alpha1.Task) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("user-initiated stop detected", "task", task.Name)

	// Suspend the Job if it exists
	if task.Status.JobName != "" {
		job := &batchv1.Job{}
		jobKey := types.NamespacedName{Name: task.Status.JobName, Namespace: task.Namespace}
		if err := r.Get(ctx, jobKey, job); err == nil {
			// Suspend the Job - Kubernetes will automatically:
			// 1. Send SIGTERM to running Pods
			// 2. Wait for graceful termination period (default 30s)
			// 3. Pod transitions to Failed state (NOT deleted)
			// 4. Logs remain accessible via kubectl logs
			suspend := true
			job.Spec.Suspend = &suspend
			if err := r.Update(ctx, job); err != nil {
				log.Error(err, "failed to suspend job")
				return ctrl.Result{}, err
			}
			log.Info("suspended job for stopped task", "job", task.Status.JobName)
		}
	}

	// Update Task status to Completed with Stopped condition
	task.Status.Phase = kubetaskv1alpha1.TaskPhaseCompleted
	task.Status.ObservedGeneration = task.Generation
	now := metav1.Now()
	task.Status.CompletionTime = &now

	meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
		Type:    "Stopped",
		Status:  metav1.ConditionTrue,
		Reason:  "UserStopped",
		Message: "Task stopped by user via kubetask.io/stop annotation",
	})

	if err := r.Status().Update(ctx, task); err != nil {
		log.Error(err, "failed to update task status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
