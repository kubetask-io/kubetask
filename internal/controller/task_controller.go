// Copyright Contributors to the KubeOpenCode project

// Package controller implements Kubernetes controllers for KubeOpenCode resources
package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kubeopenv1alpha1 "github.com/kubeopencode/kubeopencode/api/v1alpha1"
)

const (
	// DefaultAgentImage is the default OpenCode init container image.
	// This image copies the OpenCode binary to /tools volume.
	DefaultAgentImage = "quay.io/kubeopencode/kubeopencode-agent-opencode:latest"

	// DefaultExecutorImage is the default worker container image for task execution.
	// This is the development environment where tasks actually run.
	DefaultExecutorImage = "quay.io/kubeopencode/kubeopencode-agent-devbox:latest"

	// ContextConfigMapSuffix is the suffix for ConfigMap names created for context
	ContextConfigMapSuffix = "-context"

	// AgentLabelKey is the label key used to identify which Agent a Task uses
	AgentLabelKey = "kubeopencode.io/agent"

	// DefaultQueuedRequeueDelay is the default delay for requeuing queued Tasks
	DefaultQueuedRequeueDelay = 10 * time.Second

	// DefaultQuotaRequeueDelay is the minimum delay for requeuing quota-blocked Tasks
	DefaultQuotaRequeueDelay = 30 * time.Second

	// AnnotationStop is the annotation key for user-initiated task stop
	AnnotationStop = "kubeopencode.io/stop"

	// TaskFinalizer is added to Tasks when Pod runs in a different namespace
	// to ensure Pod cleanup when Task is deleted
	TaskFinalizer = "kubeopencode.io/task-cleanup"

	// TaskNamespaceLabelKey is the label key for tracking the source Task's namespace
	// when Pod runs in a different namespace (cross-namespace Agent reference)
	TaskNamespaceLabelKey = "kubeopencode.io/task-namespace"

	// RuntimeSystemPrompt is the system prompt injected when Runtime context is enabled.
	// It provides KubeOpenCode platform awareness to the agent.
	RuntimeSystemPrompt = `## KubeOpenCode Runtime Context

You are running as an AI agent inside a Kubernetes Pod, managed by KubeOpenCode.

### Environment Variables
- TASK_NAME: Name of the current Task CR
- TASK_NAMESPACE: Namespace of the current Task CR
- WORKSPACE_DIR: Working directory where task.md and context files are mounted

### Getting More Information
To get full Task specification:
  kubectl get task ${TASK_NAME} -n ${TASK_NAMESPACE} -o yaml

To get Task status:
  kubectl get task ${TASK_NAME} -n ${TASK_NAMESPACE} -o jsonpath='{.status}'

### File Structure
- ${WORKSPACE_DIR}/task.md: Your task instructions (description only)
- ${WORKSPACE_DIR}/.kubeopencode/context.md: KubeOpenCode context (loaded via OpenCode instructions)
- Additional contexts may be mounted as separate files
- Note: Repository's AGENTS.md/CLAUDE.md files are preserved and loaded by OpenCode automatically

### KubeOpenCode Concepts
- Task: Single AI task execution (what you're running now)
- Agent: Configuration for how tasks are executed (image, credentials, etc.)
`
)

// TaskReconciler reconciles a Task object
type TaskReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kubeopencode.io,resources=tasks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubeopencode.io,resources=tasks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubeopencode.io,resources=tasks/finalizers,verbs=update
// +kubebuilder:rbac:groups=kubeopencode.io,resources=agents,verbs=get;list;watch
// +kubebuilder:rbac:groups=kubeopencode.io,resources=agents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubeopencode.io,resources=kubeopencodeconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;delete

// Reconcile is part of the main kubernetes reconciliation loop
func (r *TaskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Get Task CR
	task := &kubeopenv1alpha1.Task{}
	if err := r.Get(ctx, req.NamespacedName, task); err != nil {
		if errors.IsNotFound(err) {
			// Task deleted, nothing to do
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch Task")
		return ctrl.Result{}, err
	}

	// Handle Task deletion (finalizer for cross-namespace Pod cleanup)
	if !task.DeletionTimestamp.IsZero() {
		return r.handleTaskDeletion(ctx, task)
	}

	// If new, initialize status and create Pod
	if task.Status.Phase == "" {
		return r.initializeTask(ctx, task)
	}

	// If queued, check if capacity is available
	if task.Status.Phase == kubeopenv1alpha1.TaskPhaseQueued {
		return r.handleQueuedTask(ctx, task)
	}

	// If completed/failed, handle cleanup based on KubeOpenCodeConfig
	if task.Status.Phase == kubeopenv1alpha1.TaskPhaseCompleted ||
		task.Status.Phase == kubeopenv1alpha1.TaskPhaseFailed {
		return r.handleTaskCleanup(ctx, task)
	}

	// Check for user-initiated stop (only for Running tasks)
	if task.Status.Phase == kubeopenv1alpha1.TaskPhaseRunning {
		if task.Annotations != nil && task.Annotations[AnnotationStop] == "true" {
			return r.handleStop(ctx, task)
		}
	}

	// Update task status from Pod status
	if err := r.updateTaskStatusFromPod(ctx, task); err != nil {
		log.Error(err, "unable to update task status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// initializeTask initializes a new Task and creates its Pod
func (r *TaskReconciler) initializeTask(ctx context.Context, task *kubeopenv1alpha1.Task) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Resolve TaskTemplate if referenced and merge specs
	mergedSpec, err := r.resolveTaskTemplate(ctx, task)
	if err != nil {
		log.Error(err, "unable to resolve TaskTemplate")
		// Update task status to Failed
		task.Status.ObservedGeneration = task.Generation
		task.Status.Phase = kubeopenv1alpha1.TaskPhaseFailed
		now := metav1.Now()
		task.Status.CompletionTime = &now
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:    kubeopenv1alpha1.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  kubeopenv1alpha1.ReasonTaskTemplateError,
			Message: err.Error(),
		})

		if updateErr := r.Status().Update(ctx, task); updateErr != nil {
			log.Error(updateErr, "unable to update Task status")
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil // Don't requeue, user needs to fix TaskTemplate
	}

	// Create a working copy of the task with merged spec for Pod creation
	// This ensures the original task object's spec is not modified
	workingTask := task.DeepCopy()
	workingTask.Spec = *mergedSpec

	// Get agent configuration with name and namespace
	// agentNamespace is where the Pod will run (may differ from Task namespace)
	agentConfig, agentName, agentNamespace, err := r.getAgentConfigWithName(ctx, workingTask)
	if err != nil {
		log.Error(err, "unable to get Agent")
		// Update task status to Failed
		task.Status.ObservedGeneration = task.Generation
		task.Status.Phase = kubeopenv1alpha1.TaskPhaseFailed
		now := metav1.Now()
		task.Status.CompletionTime = &now
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:    kubeopenv1alpha1.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  kubeopenv1alpha1.ReasonAgentError,
			Message: err.Error(),
		})
		if updateErr := r.Status().Update(ctx, task); updateErr != nil {
			log.Error(updateErr, "unable to update Task status")
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil // Don't requeue, user needs to fix Agent
	}

	// Add agent label to Task
	needsUpdate := false
	if task.Labels == nil {
		task.Labels = make(map[string]string)
	}
	if task.Labels[AgentLabelKey] != agentName {
		task.Labels[AgentLabelKey] = agentName
		needsUpdate = true
	}

	if needsUpdate {
		if err := r.Update(ctx, task); err != nil {
			log.Error(err, "unable to update Task")
			return ctrl.Result{}, err
		}
		// Requeue to continue with updated task
		return ctrl.Result{Requeue: true}, nil
	}

	// Determine if this is a cross-namespace setup
	isCrossNamespace := agentNamespace != task.Namespace

	// Add finalizer for cross-namespace Pod cleanup
	if isCrossNamespace && !controllerutil.ContainsFinalizer(task, TaskFinalizer) {
		controllerutil.AddFinalizer(task, TaskFinalizer)
		if err := r.Update(ctx, task); err != nil {
			log.Error(err, "unable to add finalizer")
			return ctrl.Result{}, err
		}
		// Requeue to continue with updated task
		return ctrl.Result{Requeue: true}, nil
	}

	// Check agent capacity if MaxConcurrentTasks is set
	// Note: For cross-namespace, we check capacity in the Agent's namespace
	if agentConfig.maxConcurrentTasks != nil && *agentConfig.maxConcurrentTasks > 0 {
		hasCapacity, err := r.checkAgentCapacity(ctx, agentNamespace, agentName, *agentConfig.maxConcurrentTasks)
		if err != nil {
			log.Error(err, "unable to check agent capacity")
			return ctrl.Result{}, err
		}

		if !hasCapacity {
			// Agent is at capacity, queue the task
			log.Info("agent at capacity, queueing task", "agent", agentName, "maxConcurrent", *agentConfig.maxConcurrentTasks)

			task.Status.ObservedGeneration = task.Generation
			task.Status.Phase = kubeopenv1alpha1.TaskPhaseQueued

			meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
				Type:    kubeopenv1alpha1.ConditionTypeQueued,
				Status:  metav1.ConditionTrue,
				Reason:  kubeopenv1alpha1.ReasonAgentAtCapacity,
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

	// Check agent quota if configured
	// Note: This checks rate limiting for task starts within a sliding window
	if agentConfig.quota != nil {
		agent, err := r.getAgentForQuota(ctx, agentName, agentNamespace)
		if err != nil {
			log.Error(err, "unable to get Agent for quota check")
			return ctrl.Result{}, err
		}

		hasQuota, requeueDelay, err := r.checkAgentQuota(ctx, agent)
		if err != nil {
			log.Error(err, "unable to check agent quota")
			return ctrl.Result{}, err
		}

		if !hasQuota {
			// Quota exceeded, queue the task
			log.Info("agent quota exceeded, queueing task",
				"agent", agentName,
				"maxTaskStarts", agentConfig.quota.MaxTaskStarts,
				"windowSeconds", agentConfig.quota.WindowSeconds)

			task.Status.ObservedGeneration = task.Generation
			task.Status.Phase = kubeopenv1alpha1.TaskPhaseQueued

			meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
				Type:   kubeopenv1alpha1.ConditionTypeQueued,
				Status: metav1.ConditionTrue,
				Reason: kubeopenv1alpha1.ReasonQuotaExceeded,
				Message: fmt.Sprintf("Waiting for agent %q quota (max: %d per %ds)",
					agentName, agentConfig.quota.MaxTaskStarts, agentConfig.quota.WindowSeconds),
			})

			if err := r.Status().Update(ctx, task); err != nil {
				log.Error(err, "unable to update Task status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{RequeueAfter: requeueDelay}, nil
		}
	}

	// Generate Pod name
	// For cross-namespace, include Task namespace to avoid name conflicts
	var podName string
	if isCrossNamespace {
		podName = fmt.Sprintf("%s-%s-pod", task.Namespace, task.Name)
	} else {
		podName = fmt.Sprintf("%s-pod", task.Name)
	}

	// Check if Pod already exists (in Agent's namespace)
	existingPod := &corev1.Pod{}
	podKey := types.NamespacedName{Name: podName, Namespace: agentNamespace}
	if err := r.Get(ctx, podKey, existingPod); err == nil {
		// Pod already exists, update status
		task.Status.ObservedGeneration = task.Generation
		task.Status.PodName = podName
		task.Status.PodNamespace = agentNamespace
		task.Status.Phase = kubeopenv1alpha1.TaskPhaseRunning
		now := metav1.Now()
		task.Status.StartTime = &now
		return ctrl.Result{}, r.Status().Update(ctx, task)
	}

	// Process all contexts using priority-based resolution
	// Priority (lowest to highest):
	//   1. Agent.contexts (Agent-level Context CRD references)
	//   2. TaskTemplate.contexts (Template-level defaults, if taskTemplateRef is set)
	//   3. Task.contexts (Task-specific Context CRD references)
	//   4. Task.description (highest, becomes start of ${WORKSPACE_DIR}/task.md)
	// Note: workingTask has merged spec from TaskTemplate (if any)
	// Note: For cross-namespace, Task ConfigMap contexts are read from Task namespace
	// and embedded into the ConfigMap created in Agent namespace
	contextConfigMap, fileMounts, dirMounts, gitMounts, err := r.processAllContexts(ctx, workingTask, agentConfig, agentNamespace)
	if err != nil {
		log.Error(err, "unable to process contexts")
		// Update task status to Failed - context errors are user configuration issues
		task.Status.ObservedGeneration = task.Generation
		task.Status.Phase = kubeopenv1alpha1.TaskPhaseFailed
		now := metav1.Now()
		task.Status.CompletionTime = &now
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:    kubeopenv1alpha1.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  kubeopenv1alpha1.ReasonTaskTemplateError,
			Message: err.Error(),
		})

		if updateErr := r.Status().Update(ctx, task); updateErr != nil {
			log.Error(updateErr, "unable to update Task status")
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil // Don't requeue, user needs to fix context configuration
	}

	// Create ConfigMap in Agent's namespace (where Pod runs)
	if contextConfigMap != nil {
		if err := r.Create(ctx, contextConfigMap); err != nil {
			if !errors.IsAlreadyExists(err) {
				log.Error(err, "unable to create context ConfigMap")
				return ctrl.Result{}, err
			}
		}
	}

	// Get system configuration (image, pull policies)
	// Use Agent's namespace for system config lookup
	sysCfg := r.getSystemConfig(ctx, agentNamespace)

	// Create Pod with agent configuration, context mounts, and output collector sidecar
	// Pod is created in Agent's namespace
	// Use workingTask which has merged spec from TaskTemplate (if any)
	pod := buildPod(workingTask, podName, agentNamespace, agentConfig, contextConfigMap, fileMounts, dirMounts, gitMounts, workingTask.Spec.Outputs, sysCfg)

	if err := r.Create(ctx, pod); err != nil {
		log.Error(err, "unable to create Pod", "pod", podName, "namespace", agentNamespace)
		return ctrl.Result{}, err
	}

	// Record task start for quota tracking (if quota is configured)
	if agentConfig.quota != nil {
		agent, err := r.getAgentForQuota(ctx, agentName, agentNamespace)
		if err != nil {
			log.Error(err, "unable to get Agent for quota recording")
			// Non-fatal: Pod is already created, quota tracking may be incomplete
		} else {
			if err := r.recordTaskStart(ctx, agent, task); err != nil {
				log.Error(err, "failed to record task start for quota")
				// Non-fatal: Pod is already created, quota tracking may be incomplete
			}
		}
	}

	// Update status
	task.Status.ObservedGeneration = task.Generation
	task.Status.PodName = podName
	task.Status.PodNamespace = agentNamespace
	task.Status.Phase = kubeopenv1alpha1.TaskPhaseRunning
	now := metav1.Now()
	task.Status.StartTime = &now

	if err := r.Status().Update(ctx, task); err != nil {
		log.Error(err, "unable to update Task status")
		return ctrl.Result{}, err
	}

	log.Info("initialized Task", "pod", podName, "image", agentConfig.agentImage)
	return ctrl.Result{}, nil
}

// updateTaskStatusFromPod syncs task status from Pod status
func (r *TaskReconciler) updateTaskStatusFromPod(ctx context.Context, task *kubeopenv1alpha1.Task) error {
	log := log.FromContext(ctx)

	if task.Status.PodName == "" {
		return nil
	}

	// Get Pod status from the correct namespace
	// PodNamespace may differ from Task namespace when using cross-namespace Agent
	podNamespace := task.Status.PodNamespace
	if podNamespace == "" {
		podNamespace = task.Namespace // Backwards compatibility
	}

	pod := &corev1.Pod{}
	podKey := types.NamespacedName{Name: task.Status.PodName, Namespace: podNamespace}
	if err := r.Get(ctx, podKey, pod); err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "Pod not found", "pod", task.Status.PodName)
			return nil
		}
		return err
	}

	// Check Pod phase
	switch pod.Status.Phase {
	case corev1.PodSucceeded:
		task.Status.ObservedGeneration = task.Generation
		task.Status.Phase = kubeopenv1alpha1.TaskPhaseCompleted
		now := metav1.Now()
		task.Status.CompletionTime = &now
		// Capture outputs from termination message
		task.Status.Outputs = r.captureTaskOutputs(pod)
		log.Info("task completed", "pod", task.Status.PodName, "hasOutputs", task.Status.Outputs != nil)
		return r.Status().Update(ctx, task)
	case corev1.PodFailed:
		task.Status.ObservedGeneration = task.Generation
		task.Status.Phase = kubeopenv1alpha1.TaskPhaseFailed
		now := metav1.Now()
		task.Status.CompletionTime = &now
		// Capture outputs from termination message (may contain error info)
		task.Status.Outputs = r.captureTaskOutputs(pod)
		log.Info("task failed", "pod", task.Status.PodName, "hasOutputs", task.Status.Outputs != nil)
		return r.Status().Update(ctx, task)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *TaskReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubeopenv1alpha1.Task{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}

// getAgentConfigWithName retrieves the agent configuration and returns the agent name and namespace.
// Supports cross-namespace Agent references: when Agent is in a different namespace,
// the Pod will run in the Agent's namespace to keep credentials isolated.
// Returns: agentConfig, agentName, agentNamespace (where Pod will run), error
func (r *TaskReconciler) getAgentConfigWithName(ctx context.Context, task *kubeopenv1alpha1.Task) (agentConfig, string, string, error) {
	log := log.FromContext(ctx)

	// Determine which Agent to use and its namespace
	agentName := "default"
	agentNamespace := task.Namespace // Default: same namespace as Task

	if task.Spec.AgentRef != nil {
		agentName = task.Spec.AgentRef.Name
		if task.Spec.AgentRef.Namespace != "" {
			agentNamespace = task.Spec.AgentRef.Namespace
		}
	}

	// Get Agent from agentNamespace
	agent := &kubeopenv1alpha1.Agent{}
	agentKey := types.NamespacedName{
		Name:      agentName,
		Namespace: agentNamespace,
	}

	if err := r.Get(ctx, agentKey, agent); err != nil {
		log.Error(err, "unable to get Agent", "agent", agentName, "namespace", agentNamespace)
		return agentConfig{}, "", "", fmt.Errorf("agent %q not found in namespace %q: %w", agentName, agentNamespace, err)
	}

	// Validate AllowedNamespaces when cross-namespace reference
	if agentNamespace != task.Namespace {
		if err := r.validateNamespaceAccess(agent, task.Namespace); err != nil {
			log.Error(err, "namespace access denied", "agent", agentName, "agentNamespace", agentNamespace, "taskNamespace", task.Namespace)
			return agentConfig{}, "", "", err
		}
	}

	// Get agent image (optional, has default)
	// This is the OpenCode init container image that copies the binary to /tools
	agentImage := DefaultAgentImage
	if agent.Spec.AgentImage != "" {
		agentImage = agent.Spec.AgentImage
	}

	// Get executor image (optional, has default)
	// This is the worker container image where tasks actually run
	executorImage := DefaultExecutorImage
	if agent.Spec.ExecutorImage != "" {
		executorImage = agent.Spec.ExecutorImage
	}

	// Get workspace directory (required)
	workspaceDir := agent.Spec.WorkspaceDir

	// ServiceAccountName is required
	if agent.Spec.ServiceAccountName == "" {
		return agentConfig{}, "", "", fmt.Errorf("agent %q is missing required field serviceAccountName", agentName)
	}

	return agentConfig{
		agentImage:         agentImage,
		executorImage:      executorImage,
		command:            agent.Spec.Command,
		workspaceDir:       workspaceDir,
		contexts:           agent.Spec.Contexts,
		config:             agent.Spec.Config,
		credentials:        agent.Spec.Credentials,
		podSpec:            agent.Spec.PodSpec,
		serviceAccountName: agent.Spec.ServiceAccountName,
		maxConcurrentTasks: agent.Spec.MaxConcurrentTasks,
		quota:              agent.Spec.Quota,
	}, agentName, agentNamespace, nil
}

// getAgentForQuota fetches the Agent object for quota operations.
// This is separate from getAgentConfigWithName to allow quota status updates
// while minimizing changes to existing code paths.
func (r *TaskReconciler) getAgentForQuota(ctx context.Context, agentName, agentNamespace string) (*kubeopenv1alpha1.Agent, error) {
	agent := &kubeopenv1alpha1.Agent{}
	agentKey := types.NamespacedName{
		Name:      agentName,
		Namespace: agentNamespace,
	}

	if err := r.Get(ctx, agentKey, agent); err != nil {
		return nil, err
	}

	return agent, nil
}

// validateNamespaceAccess checks if a Task's namespace is allowed to use the Agent.
// Returns nil if allowed, error if denied.
func (r *TaskReconciler) validateNamespaceAccess(agent *kubeopenv1alpha1.Agent, taskNamespace string) error {
	// Empty AllowedNamespaces means all namespaces are allowed
	if len(agent.Spec.AllowedNamespaces) == 0 {
		return nil
	}

	// Check if taskNamespace matches any pattern in AllowedNamespaces
	for _, pattern := range agent.Spec.AllowedNamespaces {
		matched, err := filepath.Match(pattern, taskNamespace)
		if err != nil {
			// Invalid pattern, treat as no match
			continue
		}
		if matched {
			return nil
		}
	}

	return fmt.Errorf("namespace %q is not allowed to use Agent %q (allowed: %v)", taskNamespace, agent.Name, agent.Spec.AllowedNamespaces)
}

// resolveTaskTemplate fetches the TaskTemplate if referenced and returns a merged TaskSpec.
// If no template is referenced, returns the original task spec unchanged.
// The merge strategy is:
//   - agentRef: Task takes precedence over Template
//   - contexts: Template contexts are prepended to Task contexts
//   - outputs: Parameters are merged, Task takes precedence for same-named params
//   - description: Task takes precedence over Template
func (r *TaskReconciler) resolveTaskTemplate(ctx context.Context, task *kubeopenv1alpha1.Task) (*kubeopenv1alpha1.TaskSpec, error) {
	if task.Spec.TaskTemplateRef == nil {
		// No template reference, return original spec
		return &task.Spec, nil
	}

	log := log.FromContext(ctx)

	// Determine template namespace
	templateNamespace := task.Namespace
	if task.Spec.TaskTemplateRef.Namespace != "" {
		templateNamespace = task.Spec.TaskTemplateRef.Namespace
	}

	// Fetch the TaskTemplate
	template := &kubeopenv1alpha1.TaskTemplate{}
	templateKey := types.NamespacedName{
		Name:      task.Spec.TaskTemplateRef.Name,
		Namespace: templateNamespace,
	}

	if err := r.Get(ctx, templateKey, template); err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("TaskTemplate %q not found in namespace %q", task.Spec.TaskTemplateRef.Name, templateNamespace)
		}
		return nil, fmt.Errorf("failed to get TaskTemplate: %w", err)
	}

	log.Info("merging Task with TaskTemplate", "template", templateKey)

	// Merge the specs
	merged := &kubeopenv1alpha1.TaskSpec{}

	// 1. AgentRef: Task takes precedence
	if task.Spec.AgentRef != nil {
		merged.AgentRef = task.Spec.AgentRef.DeepCopy()
	} else if template.Spec.AgentRef != nil {
		merged.AgentRef = template.Spec.AgentRef.DeepCopy()
	}

	// 2. Contexts: Template contexts first, then Task contexts
	merged.Contexts = make([]kubeopenv1alpha1.ContextItem, 0, len(template.Spec.Contexts)+len(task.Spec.Contexts))
	for _, c := range template.Spec.Contexts {
		merged.Contexts = append(merged.Contexts, *c.DeepCopy())
	}
	for _, c := range task.Spec.Contexts {
		merged.Contexts = append(merged.Contexts, *c.DeepCopy())
	}

	// 3. Outputs: Merge parameters, Task takes precedence for same-named params
	merged.Outputs = mergeOutputSpecs(template.Spec.Outputs, task.Spec.Outputs)

	// 4. Description: Task takes precedence
	if task.Spec.Description != nil {
		merged.Description = task.Spec.Description
	} else if template.Spec.Description != nil {
		merged.Description = template.Spec.Description
	}

	// Keep the TaskTemplateRef reference in merged spec
	merged.TaskTemplateRef = task.Spec.TaskTemplateRef

	return merged, nil
}

// mergeOutputSpecs merges two OutputSpecs, with override taking precedence for same-named params.
func mergeOutputSpecs(base, override *kubeopenv1alpha1.OutputSpec) *kubeopenv1alpha1.OutputSpec {
	if base == nil && override == nil {
		return nil
	}
	if base == nil {
		return override.DeepCopy()
	}
	if override == nil {
		return base.DeepCopy()
	}

	// Build parameter map: base first, then override
	paramMap := make(map[string]kubeopenv1alpha1.OutputParameterSpec)
	for _, p := range base.Parameters {
		paramMap[p.Name] = p
	}
	for _, p := range override.Parameters {
		paramMap[p.Name] = p
	}

	// Convert back to slice, maintaining stable order
	result := &kubeopenv1alpha1.OutputSpec{
		Parameters: make([]kubeopenv1alpha1.OutputParameterSpec, 0, len(paramMap)),
	}

	// First add base params in order (preserving order for those not overridden)
	seen := make(map[string]bool)
	for _, p := range base.Parameters {
		result.Parameters = append(result.Parameters, paramMap[p.Name])
		seen[p.Name] = true
	}
	// Then add any new params from override
	for _, p := range override.Parameters {
		if !seen[p.Name] {
			result.Parameters = append(result.Parameters, p)
		}
	}

	return result
}

// handleTaskDeletion handles Task deletion, cleaning up cross-namespace Pods.
// When Pod runs in a different namespace (cross-namespace Agent), we can't use
// OwnerReference for automatic cleanup, so we use a finalizer instead.
func (r *TaskReconciler) handleTaskDeletion(ctx context.Context, task *kubeopenv1alpha1.Task) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(task, TaskFinalizer) {
		// No finalizer, nothing to clean up
		return ctrl.Result{}, nil
	}

	log.Info("cleaning up cross-namespace Pod for deleted Task", "task", task.Name)

	// Delete Pod in the execution namespace
	if task.Status.PodName != "" && task.Status.PodNamespace != "" {
		pod := &corev1.Pod{}
		podKey := types.NamespacedName{Name: task.Status.PodName, Namespace: task.Status.PodNamespace}
		if err := r.Get(ctx, podKey, pod); err == nil {
			if err := r.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
				log.Error(err, "failed to delete cross-namespace Pod")
				return ctrl.Result{}, err
			}
			log.Info("deleted cross-namespace Pod", "pod", task.Status.PodName, "namespace", task.Status.PodNamespace)
		}

		// Also delete the context ConfigMap in the execution namespace
		configMapName := task.Name + ContextConfigMapSuffix
		cm := &corev1.ConfigMap{}
		cmKey := types.NamespacedName{Name: configMapName, Namespace: task.Status.PodNamespace}
		if err := r.Get(ctx, cmKey, cm); err == nil {
			if err := r.Delete(ctx, cm); err != nil && !errors.IsNotFound(err) {
				log.Error(err, "failed to delete cross-namespace ConfigMap")
				// Don't fail on ConfigMap deletion error, Pod is the critical resource
			}
		}
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(task, TaskFinalizer)
	if err := r.Update(ctx, task); err != nil {
		log.Error(err, "failed to remove finalizer")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// processAllContexts processes all contexts from Agent and Task
// and returns the ConfigMap, file mounts, directory mounts, and git mounts for the Pod.
//
// Content order in task.md (top to bottom):
//  1. Task.description (appears first in task.md)
//  2. Agent.contexts (Agent-level contexts)
//  3. Task.contexts (Task-specific contexts, appears last)
//
// The agentNamespace parameter specifies where the Pod runs (and where ConfigMap is created).
// For cross-namespace Agent references, this differs from task.Namespace.
func (r *TaskReconciler) processAllContexts(ctx context.Context, task *kubeopenv1alpha1.Task, cfg agentConfig, agentNamespace string) (*corev1.ConfigMap, []fileMount, []dirMount, []gitMount, error) {
	var resolved []resolvedContext
	var dirMounts []dirMount
	var gitMounts []gitMount

	// 1. Resolve Agent.contexts (appears after description in task.md)
	// Agent contexts are resolved from Agent's namespace
	for i, item := range cfg.contexts {
		rc, dm, gm, err := r.resolveContextItem(ctx, &item, agentNamespace, cfg.workspaceDir)
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
	// Task contexts are resolved from Task's namespace (may differ from Agent namespace)
	for i, item := range task.Spec.Contexts {
		rc, dm, gm, err := r.resolveContextItem(ctx, &item, task.Namespace, cfg.workspaceDir)
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
	// - Contexts without mountPath are written to .kubeopencode/context.md with XML tags
	//   (loaded via OpenCode's instructions config, avoiding conflicts with repo's AGENTS.md)
	// - task.md contains only the description
	configMapData := make(map[string]string)
	var fileMounts []fileMount

	// Build task.md content: description only
	var taskMdParts []string
	if taskDescription != "" {
		taskMdParts = append(taskMdParts, taskDescription)
	}

	// Build context file content: contexts without mountPath
	// Context is written to .kubeopencode/context.md to avoid conflicts with repository's AGENTS.md
	var contextParts []string

	for _, rc := range resolved {
		if rc.mountPath != "" {
			// Context has explicit mountPath - create separate file
			configMapKey := sanitizeConfigMapKey(rc.mountPath)
			configMapData[configMapKey] = rc.content
			fileMounts = append(fileMounts, fileMount{filePath: rc.mountPath, fileMode: rc.fileMode})
		} else {
			// No mountPath - append to .kubeopencode/context.md with XML tags
			// OpenCode loads this via OPENCODE_CONFIG_CONTENT instructions injection
			xmlTag := fmt.Sprintf("<context name=%q namespace=%q type=%q>\n%s\n</context>",
				rc.name, rc.namespace, rc.ctxType, rc.content)
			contextParts = append(contextParts, xmlTag)
		}
	}

	// Create task.md if there's any content (description only)
	// Mount at the configured workspace directory
	taskMdPath := cfg.workspaceDir + "/task.md"
	if len(taskMdParts) > 0 {
		taskMdContent := strings.Join(taskMdParts, "\n\n")
		configMapData["workspace-task.md"] = taskMdContent
		fileMounts = append(fileMounts, fileMount{filePath: taskMdPath})
	}

	// Create context file if there's any context content
	// Written to .kubeopencode/context.md to avoid conflicts with repository's AGENTS.md/CLAUDE.md
	// OpenCode loads this via OPENCODE_CONFIG_CONTENT env var with instructions config
	contextFilePath := cfg.workspaceDir + "/" + ContextFileRelPath
	if len(contextParts) > 0 {
		contextContent := strings.Join(contextParts, "\n\n")
		configMapData[sanitizeConfigMapKey(contextFilePath)] = contextContent
		fileMounts = append(fileMounts, fileMount{filePath: contextFilePath})
	}

	// Add OpenCode config to ConfigMap if provided
	if cfg.config != nil && *cfg.config != "" {
		// Validate JSON syntax
		var jsonCheck interface{}
		if err := json.Unmarshal([]byte(*cfg.config), &jsonCheck); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("invalid JSON in Agent config: %w", err)
		}
		// Use sanitizeConfigMapKey to ensure consistent key naming with fileMount
		configMapKey := sanitizeConfigMapKey(OpenCodeConfigPath)
		configMapData[configMapKey] = *cfg.config
		fileMounts = append(fileMounts, fileMount{filePath: OpenCodeConfigPath})
	}

	// Create ConfigMap if there's any content
	// ConfigMap is created in Agent's namespace (where Pod runs)
	var configMap *corev1.ConfigMap
	if len(configMapData) > 0 {
		configMapName := task.Name + ContextConfigMapSuffix
		configMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: agentNamespace, // Create in Agent's namespace
				Labels: map[string]string{
					"app":                  "kubeopencode",
					"kubeopencode.io/task": task.Name,
					TaskNamespaceLabelKey:  task.Namespace, // Track source Task namespace
				},
			},
			Data: configMapData,
		}
		// Only set OwnerReference if same namespace (cross-namespace owner refs not allowed)
		if agentNamespace == task.Namespace {
			configMap.OwnerReferences = []metav1.OwnerReference{
				{
					APIVersion: task.APIVersion,
					Kind:       task.Kind,
					Name:       task.Name,
					UID:        task.UID,
					Controller: boolPtr(true),
				},
			}
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

// resolveContextItem resolves a ContextItem to its content, directory mount, or git mount.
func (r *TaskReconciler) resolveContextItem(ctx context.Context, item *kubeopenv1alpha1.ContextItem, defaultNS, workspaceDir string) (*resolvedContext, *dirMount, *gitMount, error) {
	// Validate: Git context requires mountPath to be specified
	// Without mountPath, multiple Git contexts would conflict with the default "git-context" path.
	if item.Type == kubeopenv1alpha1.ContextTypeGit && item.MountPath == "" {
		return nil, nil, nil, fmt.Errorf("git context requires mountPath to be specified")
	}

	// Use a generated name for contexts
	// For Runtime context, use "runtime" as a more descriptive name
	name := "context"
	if item.Type == kubeopenv1alpha1.ContextTypeRuntime {
		name = "runtime"
	}

	// Resolve mountPath: relative paths are prefixed with workspaceDir
	// Note: For Runtime context, mountPath is ignored - content is always appended to task.md
	resolvedPath := resolveMountPath(item.MountPath, workspaceDir)
	if item.Type == kubeopenv1alpha1.ContextTypeRuntime {
		resolvedPath = "" // Force empty to ensure content is appended to task.md
	}

	// Resolve content based on context type
	content, dm, gm, err := r.resolveContextContent(ctx, defaultNS, name, workspaceDir, item, resolvedPath)
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
		fileMode:  item.FileMode,
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

// resolveContextContent resolves content from a ContextItem.
// Returns: content string, dirMount pointer, gitMount pointer, error
func (r *TaskReconciler) resolveContextContent(ctx context.Context, namespace, name, workspaceDir string, item *kubeopenv1alpha1.ContextItem, mountPath string) (string, *dirMount, *gitMount, error) {
	switch item.Type {
	case kubeopenv1alpha1.ContextTypeText:
		if item.Text == "" {
			return "", nil, nil, nil
		}
		return item.Text, nil, nil, nil

	case kubeopenv1alpha1.ContextTypeConfigMap:
		if item.ConfigMap == nil {
			return "", nil, nil, nil
		}
		cm := item.ConfigMap

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

	case kubeopenv1alpha1.ContextTypeGit:
		if item.Git == nil {
			return "", nil, nil, nil
		}
		git := item.Git

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

	case kubeopenv1alpha1.ContextTypeRuntime:
		// Runtime context returns the hardcoded system prompt
		// MountPath is ignored for Runtime context - content is always appended to task.md
		return RuntimeSystemPrompt, nil, nil, nil

	default:
		return "", nil, nil, fmt.Errorf("unknown context type: %s", item.Type)
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
	taskList := &kubeopenv1alpha1.TaskList{}
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
		if task.Status.Phase == kubeopenv1alpha1.TaskPhaseRunning {
			runningCount++
		}
	}

	log.V(1).Info("agent capacity check", "agent", agentName, "running", runningCount, "max", maxConcurrent)

	return runningCount < maxConcurrent, nil
}

// handleQueuedTask checks if a queued task can now be started
func (r *TaskReconciler) handleQueuedTask(ctx context.Context, task *kubeopenv1alpha1.Task) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Get agent configuration with name and namespace
	agentConfig, agentName, agentNamespace, err := r.getAgentConfigWithName(ctx, task)
	if err != nil {
		log.Error(err, "unable to get Agent for queued task")
		// Agent might be deleted, fail the task
		task.Status.Phase = kubeopenv1alpha1.TaskPhaseFailed
		now := metav1.Now()
		task.Status.CompletionTime = &now
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:    kubeopenv1alpha1.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  kubeopenv1alpha1.ReasonAgentError,
			Message: err.Error(),
		})
		if updateErr := r.Status().Update(ctx, task); updateErr != nil {
			log.Error(updateErr, "unable to update Task status")
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil
	}

	// Check if agent still has MaxConcurrentTasks set
	hasCapacityLimit := agentConfig.maxConcurrentTasks != nil && *agentConfig.maxConcurrentTasks > 0
	hasQuotaLimit := agentConfig.quota != nil

	// If neither limit is configured, proceed to initialize
	if !hasCapacityLimit && !hasQuotaLimit {
		log.Info("no limits configured, proceeding with task", "agent", agentName)
		task.Status.Phase = ""
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:    kubeopenv1alpha1.ConditionTypeQueued,
			Status:  metav1.ConditionFalse,
			Reason:  kubeopenv1alpha1.ReasonNoLimits,
			Message: fmt.Sprintf("Agent %q has no capacity or quota limits", agentName),
		})
		if err := r.Status().Update(ctx, task); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Check capacity if limit is set
	if hasCapacityLimit {
		hasCapacity, err := r.checkAgentCapacity(ctx, agentNamespace, agentName, *agentConfig.maxConcurrentTasks)
		if err != nil {
			log.Error(err, "unable to check agent capacity")
			return ctrl.Result{}, err
		}

		if !hasCapacity {
			// Still at capacity, requeue
			log.V(1).Info("agent still at capacity, remaining queued", "agent", agentName)
			return ctrl.Result{RequeueAfter: DefaultQueuedRequeueDelay}, nil
		}
	}

	// Check agent quota if configured
	if agentConfig.quota != nil {
		agent, err := r.getAgentForQuota(ctx, agentName, agentNamespace)
		if err != nil {
			log.Error(err, "unable to get Agent for quota check")
			return ctrl.Result{}, err
		}

		hasQuota, requeueDelay, err := r.checkAgentQuota(ctx, agent)
		if err != nil {
			log.Error(err, "unable to check agent quota")
			return ctrl.Result{}, err
		}

		if !hasQuota {
			// Quota still exceeded, update condition and requeue
			log.V(1).Info("agent quota still exceeded, remaining queued",
				"agent", agentName,
				"maxTaskStarts", agentConfig.quota.MaxTaskStarts,
				"windowSeconds", agentConfig.quota.WindowSeconds)

			meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
				Type:   kubeopenv1alpha1.ConditionTypeQueued,
				Status: metav1.ConditionTrue,
				Reason: kubeopenv1alpha1.ReasonQuotaExceeded,
				Message: fmt.Sprintf("Waiting for agent %q quota (max: %d per %ds)",
					agentName, agentConfig.quota.MaxTaskStarts, agentConfig.quota.WindowSeconds),
			})

			if err := r.Status().Update(ctx, task); err != nil {
				log.Error(err, "unable to update queued task status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{RequeueAfter: requeueDelay}, nil
		}
	}

	// Capacity available, transition to empty phase to trigger initializeTask
	log.Info("agent capacity available, transitioning to initialize", "agent", agentName)
	task.Status.Phase = ""
	meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
		Type:    kubeopenv1alpha1.ConditionTypeQueued,
		Status:  metav1.ConditionFalse,
		Reason:  kubeopenv1alpha1.ReasonCapacityAvailable,
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
// It deletes the Pod which triggers graceful termination via SIGTERM.
// The Pod is deleted but logs may remain accessible for a short period via kubectl logs.
func (r *TaskReconciler) handleStop(ctx context.Context, task *kubeopenv1alpha1.Task) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("user-initiated stop detected", "task", task.Name)

	// Delete the Pod if it exists
	if task.Status.PodName != "" {
		pod := &corev1.Pod{}
		podKey := types.NamespacedName{Name: task.Status.PodName, Namespace: task.Namespace}
		if err := r.Get(ctx, podKey, pod); err == nil {
			// Delete the Pod - Kubernetes will automatically:
			// 1. Send SIGTERM to the Pod
			// 2. Wait for graceful termination period (default 30s)
			// 3. Forcefully kill if still running
			if err := r.Delete(ctx, pod); err != nil {
				log.Error(err, "failed to delete pod")
				return ctrl.Result{}, err
			}
			log.Info("deleted pod for stopped task", "pod", task.Status.PodName)
		}
	}

	// Update Task status to Completed with Stopped condition
	task.Status.Phase = kubeopenv1alpha1.TaskPhaseCompleted
	task.Status.ObservedGeneration = task.Generation
	now := metav1.Now()
	task.Status.CompletionTime = &now

	meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
		Type:    kubeopenv1alpha1.ConditionTypeStopped,
		Status:  metav1.ConditionTrue,
		Reason:  kubeopenv1alpha1.ReasonUserStopped,
		Message: "Task stopped by user via kubeopencode.io/stop annotation",
	})

	if err := r.Status().Update(ctx, task); err != nil {
		log.Error(err, "failed to update task status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// getSystemConfig retrieves the system configuration from KubeOpenCodeConfig.
// It looks for config in KubeOpenCodeConfig named "default" in the task's namespace.
// Returns a systemConfig with defaults if no config is found.
func (r *TaskReconciler) getSystemConfig(ctx context.Context, namespace string) systemConfig {
	log := log.FromContext(ctx)

	// Default configuration
	cfg := systemConfig{
		systemImage:           DefaultKubeOpenCodeImage,
		systemImagePullPolicy: corev1.PullIfNotPresent,
	}

	// Try to get KubeOpenCodeConfig from the task's namespace
	config := &kubeopenv1alpha1.KubeOpenCodeConfig{}
	configKey := types.NamespacedName{Name: "default", Namespace: namespace}

	if err := r.Get(ctx, configKey, config); err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "unable to get KubeOpenCodeConfig for system config, using defaults")
		}
		// Config not found, use defaults
		return cfg
	}

	// Apply system image configuration if specified
	if config.Spec.SystemImage != nil {
		if config.Spec.SystemImage.Image != "" {
			cfg.systemImage = config.Spec.SystemImage.Image
		}
		if config.Spec.SystemImage.ImagePullPolicy != "" {
			cfg.systemImagePullPolicy = config.Spec.SystemImage.ImagePullPolicy
		}
	}

	return cfg
}

// captureTaskOutputs extracts outputs from the output-collector sidecar's termination message.
// The sidecar reads files specified in OutputSpec and writes JSON to /dev/termination-log.
// Format: {"parameters": {"key": "value"}}
func (r *TaskReconciler) captureTaskOutputs(pod *corev1.Pod) *kubeopenv1alpha1.TaskOutputs {
	// Look for the output-collector sidecar container
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == "output-collector" && cs.State.Terminated != nil {
			msg := cs.State.Terminated.Message
			if msg != "" {
				parsed := parseTerminationMessage(msg)
				if parsed != nil && len(parsed.Parameters) > 0 {
					return &kubeopenv1alpha1.TaskOutputs{
						Parameters: parsed.Parameters,
					}
				}
			}
			break
		}
	}
	return nil
}

// terminationMessageOutput represents the JSON structure written to /dev/termination-log
// by the output-collector sidecar
type terminationMessageOutput struct {
	Parameters map[string]string `json:"parameters,omitempty"`
}

// parseTerminationMessage parses the JSON termination message from the output-collector.
func parseTerminationMessage(msg string) *terminationMessageOutput {
	var output terminationMessageOutput
	if err := json.Unmarshal([]byte(msg), &output); err != nil {
		// Not valid JSON, ignore
		return nil
	}
	return &output
}

// handleTaskCleanup handles automatic cleanup of completed/failed Tasks based on KubeOpenCodeConfig.
// It checks both TTL-based and retention-based cleanup policies.
// Returns a Result with RequeueAfter if TTL cleanup is scheduled for the future.
func (r *TaskReconciler) handleTaskCleanup(ctx context.Context, task *kubeopenv1alpha1.Task) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Get cleanup configuration from KubeOpenCodeConfig
	cleanupConfig := r.getCleanupConfig(ctx, task.Namespace)
	if cleanupConfig == nil {
		// No cleanup configured, nothing to do
		return ctrl.Result{}, nil
	}

	// Check TTL-based cleanup
	if cleanupConfig.TTLSecondsAfterFinished != nil {
		result, deleted, err := r.checkTTLCleanup(ctx, task, *cleanupConfig.TTLSecondsAfterFinished)
		if err != nil {
			log.Error(err, "failed to check TTL cleanup")
			return ctrl.Result{}, err
		}
		if deleted {
			// Task was deleted, nothing more to do
			return result, nil
		}
		// If TTL is set but not expired, we need to requeue
		// Continue to check retention limit
		if result.RequeueAfter > 0 {
			// Schedule requeue for TTL expiration
			// Also check retention cleanup before returning
			if cleanupConfig.MaxRetainedTasks != nil {
				if err := r.checkRetentionCleanup(ctx, task.Namespace, *cleanupConfig.MaxRetainedTasks); err != nil {
					log.Error(err, "failed to check retention cleanup")
					return ctrl.Result{}, err
				}
			}
			return result, nil
		}
	}

	// Check retention-based cleanup
	if cleanupConfig.MaxRetainedTasks != nil {
		if err := r.checkRetentionCleanup(ctx, task.Namespace, *cleanupConfig.MaxRetainedTasks); err != nil {
			log.Error(err, "failed to check retention cleanup")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// getCleanupConfig retrieves cleanup configuration from KubeOpenCodeConfig.
// Returns nil if no cleanup is configured.
func (r *TaskReconciler) getCleanupConfig(ctx context.Context, namespace string) *kubeopenv1alpha1.CleanupConfig {
	log := log.FromContext(ctx)

	// Try to get KubeOpenCodeConfig from the task's namespace
	config := &kubeopenv1alpha1.KubeOpenCodeConfig{}
	configKey := types.NamespacedName{Name: "default", Namespace: namespace}

	if err := r.Get(ctx, configKey, config); err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "unable to get KubeOpenCodeConfig for cleanup config")
		}
		// Config not found, no cleanup configured
		return nil
	}

	return config.Spec.Cleanup
}

// checkTTLCleanup checks if the Task should be deleted based on TTL.
// Returns (result, deleted, error) where deleted is true if the Task was deleted.
func (r *TaskReconciler) checkTTLCleanup(ctx context.Context, task *kubeopenv1alpha1.Task, ttlSeconds int32) (ctrl.Result, bool, error) {
	log := log.FromContext(ctx)

	// CompletionTime must be set for TTL calculation
	if task.Status.CompletionTime == nil {
		// This shouldn't happen for completed/failed tasks, but handle gracefully
		return ctrl.Result{}, false, nil
	}

	// Calculate elapsed time since completion
	elapsed := time.Since(task.Status.CompletionTime.Time)
	ttlDuration := time.Duration(ttlSeconds) * time.Second

	if elapsed >= ttlDuration {
		// TTL expired, delete the Task
		log.Info("deleting Task due to TTL expiration",
			"task", task.Name,
			"ttlSeconds", ttlSeconds,
			"elapsedSeconds", int(elapsed.Seconds()))

		if err := r.Delete(ctx, task); err != nil {
			if errors.IsNotFound(err) {
				// Already deleted, that's fine
				return ctrl.Result{}, true, nil
			}
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	// TTL not expired yet, schedule requeue for when it expires
	remaining := ttlDuration - elapsed
	log.V(1).Info("scheduling TTL cleanup",
		"task", task.Name,
		"remainingSeconds", int(remaining.Seconds()))

	return ctrl.Result{RequeueAfter: remaining}, false, nil
}

// checkRetentionCleanup checks if any Tasks should be deleted based on retention count.
// Deletes the oldest completed/failed Tasks (by CompletionTime) if count exceeds limit.
func (r *TaskReconciler) checkRetentionCleanup(ctx context.Context, namespace string, maxRetained int32) error {
	log := log.FromContext(ctx)

	// List all Tasks in the namespace
	taskList := &kubeopenv1alpha1.TaskList{}
	if err := r.List(ctx, taskList, client.InNamespace(namespace)); err != nil {
		return err
	}

	// Filter completed/failed Tasks with CompletionTime set
	var completedTasks []kubeopenv1alpha1.Task
	for _, t := range taskList.Items {
		if (t.Status.Phase == kubeopenv1alpha1.TaskPhaseCompleted ||
			t.Status.Phase == kubeopenv1alpha1.TaskPhaseFailed) &&
			t.Status.CompletionTime != nil {
			completedTasks = append(completedTasks, t)
		}
	}

	// Check if we're over the limit
	excess := len(completedTasks) - int(maxRetained)
	if excess <= 0 {
		// Under limit, nothing to delete
		return nil
	}

	// Sort by CompletionTime (oldest first)
	sort.Slice(completedTasks, func(i, j int) bool {
		return completedTasks[i].Status.CompletionTime.Before(completedTasks[j].Status.CompletionTime)
	})

	// Delete excess Tasks (oldest first)
	log.Info("deleting Tasks due to retention limit",
		"namespace", namespace,
		"count", len(completedTasks),
		"maxRetained", maxRetained,
		"toDelete", excess)

	for i := 0; i < excess; i++ {
		taskToDelete := &completedTasks[i]
		log.Info("deleting Task due to retention limit",
			"task", taskToDelete.Name,
			"completionTime", taskToDelete.Status.CompletionTime.Time)

		if err := r.Delete(ctx, taskToDelete); err != nil {
			if errors.IsNotFound(err) {
				// Already deleted, continue
				continue
			}
			return err
		}
	}

	return nil
}

// pruneTaskStartHistory removes expired records from TaskStartHistory.
// Records older than the quota window are removed.
func pruneTaskStartHistory(history []kubeopenv1alpha1.TaskStartRecord, windowSeconds int32) []kubeopenv1alpha1.TaskStartRecord {
	windowStart := time.Now().Add(-time.Duration(windowSeconds) * time.Second)
	var pruned []kubeopenv1alpha1.TaskStartRecord
	for _, record := range history {
		if record.StartTime.Time.After(windowStart) { //nolint:staticcheck // Using embedded time.Time's After method
			pruned = append(pruned, record)
		}
	}
	return pruned
}

// getActiveRecordsInWindow returns records within the sliding window.
func getActiveRecordsInWindow(history []kubeopenv1alpha1.TaskStartRecord, windowSeconds int32) []kubeopenv1alpha1.TaskStartRecord {
	return pruneTaskStartHistory(history, windowSeconds)
}

// calculateQuotaRequeueDelay calculates when the next quota slot becomes available.
// Returns the time until the oldest record in the window expires, with a minimum
// of DefaultQuotaRequeueDelay.
func calculateQuotaRequeueDelay(history []kubeopenv1alpha1.TaskStartRecord, windowSeconds int32) time.Duration {
	if len(history) == 0 {
		return DefaultQuotaRequeueDelay
	}

	// Find the oldest record in the window
	activeRecords := getActiveRecordsInWindow(history, windowSeconds)
	if len(activeRecords) == 0 {
		return DefaultQuotaRequeueDelay
	}

	// Sort by StartTime to find the oldest
	sort.Slice(activeRecords, func(i, j int) bool {
		return activeRecords[i].StartTime.Time.Before(activeRecords[j].StartTime.Time) //nolint:staticcheck // Using embedded time.Time's Before method
	})

	// Calculate when the oldest record expires
	oldestRecord := activeRecords[0]
	windowDuration := time.Duration(windowSeconds) * time.Second
	expiresAt := oldestRecord.StartTime.Time.Add(windowDuration) //nolint:staticcheck // Using embedded time.Time's Add method
	delay := time.Until(expiresAt)

	// Ensure minimum delay
	if delay < DefaultQuotaRequeueDelay {
		delay = DefaultQuotaRequeueDelay
	}

	return delay
}

// checkAgentQuota checks if a new Task can start based on the Agent's quota.
// Returns (hasQuota, requeueAfter, error):
//   - hasQuota=true: Task can start immediately
//   - hasQuota=false: Task should be queued, requeueAfter is the suggested delay
func (r *TaskReconciler) checkAgentQuota(ctx context.Context, agent *kubeopenv1alpha1.Agent) (bool, time.Duration, error) {
	log := log.FromContext(ctx)

	if agent.Spec.Quota == nil {
		return true, 0, nil
	}

	quota := agent.Spec.Quota
	activeRecords := getActiveRecordsInWindow(agent.Status.TaskStartHistory, quota.WindowSeconds)
	currentCount := int32(len(activeRecords)) //nolint:gosec // len() is always non-negative and bounded by slice capacity

	log.V(1).Info("quota check",
		"agent", agent.Name,
		"activeCount", currentCount,
		"maxTaskStarts", quota.MaxTaskStarts,
		"windowSeconds", quota.WindowSeconds)

	if currentCount >= quota.MaxTaskStarts {
		requeueDelay := calculateQuotaRequeueDelay(agent.Status.TaskStartHistory, quota.WindowSeconds)
		return false, requeueDelay, nil
	}

	return true, 0, nil
}

// recordTaskStart adds a TaskStartRecord to the Agent's status.
// Uses retry logic for optimistic concurrency conflicts in HA mode.
func (r *TaskReconciler) recordTaskStart(ctx context.Context, agent *kubeopenv1alpha1.Agent, task *kubeopenv1alpha1.Task) error {
	log := log.FromContext(ctx)

	if agent.Spec.Quota == nil {
		return nil
	}

	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		// Fetch fresh Agent
		freshAgent := &kubeopenv1alpha1.Agent{}
		if err := r.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, freshAgent); err != nil {
			return err
		}

		// Check if quota is still configured (could be removed between retries)
		if freshAgent.Spec.Quota == nil {
			return nil
		}

		// Prune old records and add new one
		freshAgent.Status.TaskStartHistory = pruneTaskStartHistory(
			freshAgent.Status.TaskStartHistory,
			freshAgent.Spec.Quota.WindowSeconds,
		)
		freshAgent.Status.TaskStartHistory = append(freshAgent.Status.TaskStartHistory, kubeopenv1alpha1.TaskStartRecord{
			TaskName:      task.Name,
			TaskNamespace: task.Namespace,
			StartTime:     metav1.Now(),
		})

		// Update status
		if err := r.Status().Update(ctx, freshAgent); err != nil {
			if errors.IsConflict(err) {
				log.V(1).Info("conflict updating agent status, retrying", "retry", i+1)
				continue
			}
			return err
		}

		log.V(1).Info("recorded task start for quota",
			"agent", agent.Name,
			"task", task.Name,
			"historySize", len(freshAgent.Status.TaskStartHistory))
		return nil
	}

	return fmt.Errorf("failed to record task start after %d retries", maxRetries)
}
