// Copyright Contributors to the KubeTask project

// Package controller implements Kubernetes controllers for KubeTask resources
package controller

import (
	"context"
	"fmt"
	"strings"

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

	// If completed/failed, skip
	if task.Status.Phase == kubetaskv1alpha1.TaskPhaseCompleted ||
		task.Status.Phase == kubetaskv1alpha1.TaskPhaseFailed {
		return ctrl.Result{}, nil
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
	contextConfigMap, fileMounts, err := r.processContexts(ctx, task, allContexts)
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
	job := r.buildJob(task, jobName, agentConfig, contextConfigMap, fileMounts)

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
	toolsImage         string
	defaultContexts    []kubetaskv1alpha1.Context
	credentials        []kubetaskv1alpha1.Credential
	podLabels          map[string]string
	scheduling         *kubetaskv1alpha1.PodScheduling
	serviceAccountName string
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
		toolsImage:         agent.Spec.ToolsImage,
		defaultContexts:    agent.Spec.DefaultContexts,
		credentials:        agent.Spec.Credentials,
		podLabels:          agent.Spec.PodLabels,
		scheduling:         agent.Spec.Scheduling,
		serviceAccountName: agent.Spec.ServiceAccountName,
	}, nil
}

// fileMount represents a file to be mounted at a specific path
type fileMount struct {
	filePath string
}

// processContexts processes all contexts and returns:
// - ConfigMap for aggregated content (grouped by FilePath)
// - List of file mounts for the job
//
// All context types (inline, configMap, secret) are resolved and aggregated by FilePath.
// Multiple contexts with the same FilePath will have their contents merged.
func (r *TaskReconciler) processContexts(ctx context.Context, task *kubetaskv1alpha1.Task, contexts []kubetaskv1alpha1.Context) (*corev1.ConfigMap, []fileMount, error) {
	// Group resolved contents by FilePath
	// Key: filePath, Value: list of resolved contents to aggregate
	contentsByPath := make(map[string][]string)

	for _, c := range contexts {
		if c.Type != kubetaskv1alpha1.ContextTypeFile || c.File == nil {
			continue
		}

		file := c.File
		filePath := file.FilePath

		// Resolve content from any source type
		content, err := r.resolveFileContent(ctx, task.Namespace, file)
		if err != nil {
			return nil, nil, err
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

	return configMap, fileMounts, nil
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

	if file.Source.SecretKeyRef != nil {
		ref := file.Source.SecretKeyRef
		secret := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, secret); err != nil {
			if ref.Optional != nil && *ref.Optional {
				return "", nil
			}
			return "", err
		}
		if content, ok := secret.Data[ref.Key]; ok {
			return string(content), nil
		}
		if ref.Optional != nil && *ref.Optional {
			return "", nil
		}
		return "", fmt.Errorf("key %s not found in Secret %s", ref.Key, ref.Name)
	}

	return "", nil
}

// buildJob creates a Job object for the task with context mounts
func (r *TaskReconciler) buildJob(task *kubetaskv1alpha1.Task, jobName string, cfg agentConfig, contextConfigMap *corev1.ConfigMap, fileMounts []fileMount) *batchv1.Job {
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount
	var initContainers []corev1.Container
	var envVars []corev1.EnvVar

	// Base environment variables
	envVars = append(envVars,
		corev1.EnvVar{Name: "TASK_NAME", Value: task.Name},
		corev1.EnvVar{Name: "TASK_NAMESPACE", Value: task.Namespace},
	)

	// Add tools volume and initContainer if toolsImage is specified
	if cfg.toolsImage != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "tools-volume",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "tools-volume",
			MountPath: "/tools",
		})
		initContainers = append(initContainers, corev1.Container{
			Name:    "copy-tools",
			Image:   cfg.toolsImage,
			Command: []string{"sh", "-c", "cp -a /tools/. /shared-tools/"},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "tools-volume",
					MountPath: "/shared-tools",
				},
			},
		})
		// Add PATH and other environment variables for tools
		envVars = append(envVars,
			corev1.EnvVar{Name: "PATH", Value: "/tools/bin:/usr/local/bin:/usr/bin:/bin"},
			corev1.EnvVar{Name: "NODE_PATH", Value: "/tools/lib/node_modules"},
			corev1.EnvVar{Name: "LD_LIBRARY_PATH", Value: "/tools/lib"},
		)
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

	// Build pod labels - start with base labels
	podLabels := map[string]string{
		"app":              "kubetask",
		"kubetask.io/task": task.Name,
	}

	// Add custom pod labels from Agent
	for k, v := range cfg.podLabels {
		podLabels[k] = v
	}

	// Build PodSpec with scheduling configuration
	podSpec := corev1.PodSpec{
		ServiceAccountName: cfg.serviceAccountName,
		InitContainers:     initContainers,
		Containers: []corev1.Container{
			{
				Name:            "agent",
				Image:           cfg.agentImage,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env:             envVars,
				VolumeMounts:    volumeMounts,
			},
		},
		Volumes:       volumes,
		RestartPolicy: corev1.RestartPolicyNever,
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
