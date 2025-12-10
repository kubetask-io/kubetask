// Copyright Contributors to the KubeTask project

// Package controller implements Kubernetes controllers for KubeTask resources
package controller

import (
	"context"
	"fmt"
	"strconv"
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

	kubetaskv1alpha1 "github.com/xuezhaojun/kubetask/api/v1alpha1"
)

const (
	// DefaultAgentImage is the default agent container image
	DefaultAgentImage = "quay.io/zhaoxue/kubetask-agent-gemini:latest"

	// ContextConfigMapSuffix is the suffix for ConfigMap names created for context
	ContextConfigMapSuffix = "-context"

	// DefaultTTLSecondsAfterFinished is the default TTL for completed/failed tasks (7 days)
	DefaultTTLSecondsAfterFinished int32 = 604800

	// DefaultKeepAliveSeconds is the default keep-alive duration for human-in-the-loop (1 hour)
	DefaultKeepAliveSeconds int32 = 3600

	// EnvHumanInTheLoopKeepAlive is the environment variable name for keep-alive seconds
	EnvHumanInTheLoopKeepAlive = "KUBETASK_KEEP_ALIVE_SECONDS"
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

	// If completed/failed, check TTL for cleanup
	if task.Status.Phase == kubetaskv1alpha1.TaskPhaseCompleted ||
		task.Status.Phase == kubetaskv1alpha1.TaskPhaseFailed {
		return r.handleTaskCleanup(ctx, task)
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

	// Get agent configuration
	agentConfig, err := r.getAgentConfig(ctx, task)
	if err != nil {
		log.Error(err, "unable to get Agent")
		// Update task status to Failed
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

	// Generate Job name
	jobName := fmt.Sprintf("%s-job", task.Name)

	// Check if Job already exists
	existingJob := &batchv1.Job{}
	jobKey := types.NamespacedName{Name: jobName, Namespace: task.Namespace}
	if err := r.Get(ctx, jobKey, existingJob); err == nil {
		// Job already exists, update status
		task.Status.JobName = jobName
		task.Status.Phase = kubetaskv1alpha1.TaskPhaseRunning
		now := metav1.Now()
		task.Status.StartTime = &now
		return ctrl.Result{}, r.Status().Update(ctx, task)
	}

	// Merge contexts: defaultContexts + task.Spec.Contexts
	allContexts := make([]kubetaskv1alpha1.Context, 0, len(agentConfig.defaultContexts)+len(task.Spec.Contexts))
	allContexts = append(allContexts, agentConfig.defaultContexts...)
	allContexts = append(allContexts, task.Spec.Contexts...)

	// Process contexts and create ConfigMap for aggregated content
	contextConfigMap, fileMounts, dirMounts, err := r.processContexts(ctx, task, allContexts)
	if err != nil {
		log.Error(err, "unable to process contexts")
		return ctrl.Result{}, err
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
	job := r.buildJob(task, jobName, agentConfig, contextConfigMap, fileMounts, dirMounts)

	if err := r.Create(ctx, job); err != nil {
		log.Error(err, "unable to create Job", "job", jobName)
		return ctrl.Result{}, err
	}

	// Update status
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
		task.Status.Phase = kubetaskv1alpha1.TaskPhaseCompleted
		now := metav1.Now()
		task.Status.CompletionTime = &now
		log.Info("task completed", "job", task.Status.JobName)
		return r.Status().Update(ctx, task)
	} else if job.Status.Failed > 0 {
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

// agentConfig holds the resolved configuration from Agent
type agentConfig struct {
	agentImage         string
	command            []string
	defaultContexts    []kubetaskv1alpha1.Context
	credentials        []kubetaskv1alpha1.Credential
	podLabels          map[string]string
	scheduling         *kubetaskv1alpha1.PodScheduling
	serviceAccountName string
	humanInTheLoop     *kubetaskv1alpha1.HumanInTheLoop
}

// getAgentConfig retrieves the agent configuration from Agent.
// Returns an error if Agent is not found or invalid.
func (r *TaskReconciler) getAgentConfig(ctx context.Context, task *kubetaskv1alpha1.Task) (agentConfig, error) {
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
		return agentConfig{}, fmt.Errorf("Agent %q not found in namespace %q: %w", agentName, task.Namespace, err)
	}

	// Get agent image (optional, has default)
	agentImage := DefaultAgentImage
	if agent.Spec.AgentImage != "" {
		agentImage = agent.Spec.AgentImage
	}

	// ServiceAccountName is required
	if agent.Spec.ServiceAccountName == "" {
		return agentConfig{}, fmt.Errorf("Agent %q is missing required field serviceAccountName", agentName)
	}

	return agentConfig{
		agentImage:         agentImage,
		command:            agent.Spec.Command,
		defaultContexts:    agent.Spec.DefaultContexts,
		credentials:        agent.Spec.Credentials,
		podLabels:          agent.Spec.PodLabels,
		scheduling:         agent.Spec.Scheduling,
		serviceAccountName: agent.Spec.ServiceAccountName,
		humanInTheLoop:     agent.Spec.HumanInTheLoop,
	}, nil
}

// fileMount represents a file to be mounted at a specific path
type fileMount struct {
	filePath string
}

// dirMount represents a directory to be mounted from a ConfigMap
type dirMount struct {
	dirPath       string
	configMapName string
	optional      bool
}

// processContexts processes all contexts and returns:
// - ConfigMap for aggregated content (grouped by FilePath)
// - List of file mounts for the job
// - List of directory mounts (for ConfigMapRef)
//
// All context types (inline, configMap) are resolved and aggregated by FilePath.
// Multiple contexts with the same FilePath will have their contents merged.
// Directory mounts (DirPath + ConfigMapRef) are passed through directly.
func (r *TaskReconciler) processContexts(ctx context.Context, task *kubetaskv1alpha1.Task, contexts []kubetaskv1alpha1.Context) (*corev1.ConfigMap, []fileMount, []dirMount, error) {
	// Group resolved contents by FilePath
	// Key: filePath, Value: list of resolved contents to aggregate
	contentsByPath := make(map[string][]string)
	var dirMounts []dirMount

	for _, c := range contexts {
		if c.Type != kubetaskv1alpha1.ContextTypeFile || c.File == nil {
			continue
		}

		file := c.File

		// Handle directory mount (DirPath + ConfigMapRef)
		if file.DirPath != "" && file.Source.ConfigMapRef != nil {
			optional := false
			if file.Source.ConfigMapRef.Optional != nil {
				optional = *file.Source.ConfigMapRef.Optional
			}
			dirMounts = append(dirMounts, dirMount{
				dirPath:       file.DirPath,
				configMapName: file.Source.ConfigMapRef.Name,
				optional:      optional,
			})
			continue
		}

		// Handle file mount (FilePath + Inline/ConfigMapKeyRef)
		filePath := file.FilePath
		if filePath == "" {
			continue
		}

		// Resolve content from any source type
		content, err := r.resolveFileContent(ctx, task.Namespace, file)
		if err != nil {
			return nil, nil, nil, err
		}
		if content != "" {
			contentsByPath[filePath] = append(contentsByPath[filePath], content)
		}
	}

	// Build file mounts and ConfigMap data
	var fileMounts []fileMount
	configMapData := make(map[string]string)

	for filePath, contents := range contentsByPath {
		var aggregated string
		if len(contents) == 1 {
			// Single content - use as is
			aggregated = contents[0]
		} else {
			// Multiple contents - wrap each in XML tags
			var wrappedContents []string
			for i, content := range contents {
				wrapped := fmt.Sprintf("<context index=\"%d\">\n%s\n</context>", i, content)
				wrappedContents = append(wrappedContents, wrapped)
			}
			aggregated = strings.Join(wrappedContents, "\n\n")
		}

		// Use a sanitized key for ConfigMap (replace / with -)
		configMapKey := sanitizeConfigMapKey(filePath)
		configMapData[configMapKey] = aggregated

		fileMounts = append(fileMounts, fileMount{
			filePath: filePath,
		})
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

	return configMap, fileMounts, dirMounts, nil
}

// sanitizeConfigMapKey converts a file path to a valid ConfigMap key
// ConfigMap keys must be alphanumeric, '-', '_', or '.'
func sanitizeConfigMapKey(filePath string) string {
	// Remove leading slash and replace remaining slashes with dashes
	key := strings.TrimPrefix(filePath, "/")
	key = strings.ReplaceAll(key, "/", "-")
	return key
}

// resolveFileContent resolves the content of a file from its source
func (r *TaskReconciler) resolveFileContent(ctx context.Context, namespace string, file *kubetaskv1alpha1.FileContext) (string, error) {
	if file.Source.Inline != nil {
		return *file.Source.Inline, nil
	}

	if file.Source.ConfigMapKeyRef != nil {
		ref := file.Source.ConfigMapKeyRef
		cm := &corev1.ConfigMap{}
		if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, cm); err != nil {
			if ref.Optional != nil && *ref.Optional {
				return "", nil
			}
			return "", err
		}
		if content, ok := cm.Data[ref.Key]; ok {
			return content, nil
		}
		if ref.Optional != nil && *ref.Optional {
			return "", nil
		}
		return "", fmt.Errorf("key %s not found in ConfigMap %s", ref.Key, ref.Name)
	}

	// ConfigMapRef is handled separately in processContexts as a directory mount
	return "", nil
}

// buildJob creates a Job object for the task with context mounts
func (r *TaskReconciler) buildJob(task *kubetaskv1alpha1.Task, jobName string, cfg agentConfig, contextConfigMap *corev1.ConfigMap, fileMounts []fileMount, dirMounts []dirMount) *batchv1.Job {
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount
	var envVars []corev1.EnvVar

	// Base environment variables
	envVars = append(envVars,
		corev1.EnvVar{Name: "TASK_NAME", Value: task.Name},
		corev1.EnvVar{Name: "TASK_NAMESPACE", Value: task.Namespace},
	)

	// Add human-in-the-loop keep-alive environment variable if enabled
	if cfg.humanInTheLoop != nil && cfg.humanInTheLoop.Enabled {
		keepAliveSeconds := DefaultKeepAliveSeconds
		if cfg.humanInTheLoop.KeepAliveSeconds != nil {
			keepAliveSeconds = *cfg.humanInTheLoop.KeepAliveSeconds
		}
		envVars = append(envVars, corev1.EnvVar{
			Name:  EnvHumanInTheLoopKeepAlive,
			Value: strconv.Itoa(int(keepAliveSeconds)),
		})
	}

	// Add credentials (secrets as env vars or file mounts)
	for i, cred := range cfg.credentials {
		// Add as environment variable if Env is specified
		if cred.Env != nil && *cred.Env != "" {
			envVars = append(envVars, corev1.EnvVar{
				Name: *cred.Env,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: cred.SecretRef.Name,
						},
						Key: cred.SecretRef.Key,
					},
				},
			})
		}

		// Add as file mount if MountPath is specified
		if cred.MountPath != nil && *cred.MountPath != "" {
			volumeName := fmt.Sprintf("credential-%d", i)

			// Default file mode is 0600 (read/write for owner only)
			var fileMode int32 = 0600
			if cred.FileMode != nil {
				fileMode = *cred.FileMode
			}

			volumes = append(volumes, corev1.Volume{
				Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: cred.SecretRef.Name,
						Items: []corev1.KeyToPath{
							{
								Key:  cred.SecretRef.Key,
								Path: "secret-file",
								Mode: &fileMode,
							},
						},
						DefaultMode: &fileMode,
					},
				},
			})
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      volumeName,
				MountPath: *cred.MountPath,
				SubPath:   "secret-file",
			})
		}
	}

	// Add context ConfigMap volume if it exists (for aggregated content)
	if contextConfigMap != nil {
		volumes = append(volumes, corev1.Volume{
			Name: "context-files",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: contextConfigMap.Name,
					},
				},
			},
		})

		// Add volume mounts for each file path
		for _, mount := range fileMounts {
			configMapKey := sanitizeConfigMapKey(mount.filePath)
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      "context-files",
				MountPath: mount.filePath,
				SubPath:   configMapKey,
			})
		}
	}

	// Add directory mounts (ConfigMapRef - entire ConfigMap as a directory)
	for i, dm := range dirMounts {
		volumeName := fmt.Sprintf("dir-mount-%d", i)
		volumes = append(volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: dm.configMapName,
					},
					Optional: &dm.optional,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: dm.dirPath,
		})
	}

	// Build pod labels - start with base labels
	podLabels := map[string]string{
		"app":              "kubetask",
		"kubetask.io/task": task.Name,
	}

	// Add custom pod labels from Agent
	for k, v := range cfg.podLabels {
		podLabels[k] = v
	}

	// Build agent container
	agentContainer := corev1.Container{
		Name:            "agent",
		Image:           cfg.agentImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             envVars,
		VolumeMounts:    volumeMounts,
	}

	// Apply command if specified
	if len(cfg.command) > 0 {
		// If humanInTheLoop is enabled, wrap the command with sleep
		if cfg.humanInTheLoop != nil && cfg.humanInTheLoop.Enabled {
			keepAliveSeconds := DefaultKeepAliveSeconds
			if cfg.humanInTheLoop.KeepAliveSeconds != nil {
				keepAliveSeconds = *cfg.humanInTheLoop.KeepAliveSeconds
			}

			// Build the wrapped command that runs original command then sleeps
			// Format: sh -c 'original_command; EXIT_CODE=$?; echo "Human-in-the-loop: keeping container alive..."; sleep N; exit $EXIT_CODE'
			originalCmd := strings.Join(cfg.command, " ")
			wrappedScript := fmt.Sprintf(
				`%s; EXIT_CODE=$?; echo "Human-in-the-loop: keeping container alive for %d seconds. Use 'kubectl exec' to access."; sleep %d; exit $EXIT_CODE`,
				originalCmd, keepAliveSeconds, keepAliveSeconds,
			)
			agentContainer.Command = []string{"sh", "-c", wrappedScript}
		} else {
			// No humanInTheLoop, use command as-is
			agentContainer.Command = cfg.command
		}
	}

	// Build PodSpec with scheduling configuration
	podSpec := corev1.PodSpec{
		ServiceAccountName: cfg.serviceAccountName,
		Containers:         []corev1.Container{agentContainer},
		Volumes:            volumes,
		RestartPolicy:      corev1.RestartPolicyNever,
	}

	// Apply scheduling configuration if specified
	if cfg.scheduling != nil {
		if cfg.scheduling.NodeSelector != nil {
			podSpec.NodeSelector = cfg.scheduling.NodeSelector
		}
		if cfg.scheduling.Tolerations != nil {
			podSpec.Tolerations = cfg.scheduling.Tolerations
		}
		if cfg.scheduling.Affinity != nil {
			podSpec.Affinity = cfg.scheduling.Affinity
		}
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
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
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabels,
				},
				Spec: podSpec,
			},
		},
	}
}

// boolPtr returns a pointer to the given bool value
func boolPtr(b bool) *bool {
	return &b
}
