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

	// AggregatedContextPath is the default path for aggregated context file
	AggregatedContextPath = "/workspace/task.md"

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
// +kubebuilder:rbac:groups=kubetask.io,resources=workspaceconfigs,verbs=get;list;watch
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
	if task.Status.Phase == kubetaskv1alpha1.TaskPhaseSucceeded ||
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

	// Get workspace configuration
	wsConfig := r.getWorkspaceConfig(ctx, task)

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
	allContexts := make([]kubetaskv1alpha1.Context, 0, len(wsConfig.defaultContexts)+len(task.Spec.Contexts))
	allContexts = append(allContexts, wsConfig.defaultContexts...)
	allContexts = append(allContexts, task.Spec.Contexts...)

	// Process contexts and create ConfigMap for aggregated content
	contextConfigMap, explicitMounts, err := r.processContexts(ctx, task, allContexts)
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

	// Create ConfigMaps for inline content with explicit mountPath
	for i, mount := range explicitMounts {
		if mount.inlineContent != nil {
			inlineConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-inline-%d", task.Name, i),
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
				Data: map[string]string{
					mount.inlineFileName: *mount.inlineContent,
				},
			}
			if err := r.Create(ctx, inlineConfigMap); err != nil {
				if !errors.IsAlreadyExists(err) {
					log.Error(err, "unable to create inline ConfigMap", "index", i)
					return ctrl.Result{}, err
				}
			}
		}
	}

	// Create Job with workspace configuration and context mounts
	job := r.buildJob(task, jobName, wsConfig, contextConfigMap, explicitMounts)

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

	log.Info("initialized Task", "job", jobName, "image", wsConfig.agentImage)
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
		task.Status.Phase = kubetaskv1alpha1.TaskPhaseSucceeded
		now := metav1.Now()
		task.Status.CompletionTime = &now
		log.Info("task succeeded", "job", task.Status.JobName)
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

// workspaceConfig holds the resolved configuration from WorkspaceConfig
type workspaceConfig struct {
	agentImage      string
	toolsImage      string
	defaultContexts []kubetaskv1alpha1.Context
	credentials     []kubetaskv1alpha1.Credential
	podLabels       map[string]string
	scheduling      *kubetaskv1alpha1.PodScheduling
}

// getWorkspaceConfig retrieves the workspace configuration from WorkspaceConfig
func (r *TaskReconciler) getWorkspaceConfig(ctx context.Context, task *kubetaskv1alpha1.Task) workspaceConfig {
	log := log.FromContext(ctx)

	// Determine which WorkspaceConfig to use
	configName := "default"
	if task.Spec.WorkspaceConfigRef != "" {
		configName = task.Spec.WorkspaceConfigRef
	}

	// Try to get WorkspaceConfig
	config := &kubetaskv1alpha1.WorkspaceConfig{}
	configKey := types.NamespacedName{
		Name:      configName,
		Namespace: task.Namespace,
	}

	if err := r.Get(ctx, configKey, config); err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "unable to get WorkspaceConfig, using defaults", "workspaceConfig", configName)
		}
		return workspaceConfig{agentImage: DefaultAgentImage}
	}

	// Get agent image
	agentImage := DefaultAgentImage
	if config.Spec.AgentImage != "" {
		agentImage = config.Spec.AgentImage
	}

	return workspaceConfig{
		agentImage:      agentImage,
		toolsImage:      config.Spec.ToolsImage,
		defaultContexts: config.Spec.DefaultContexts,
		credentials:     config.Spec.Credentials,
		podLabels:       config.Spec.PodLabels,
		scheduling:      config.Spec.Scheduling,
	}
}

// explicitMount represents a file that should be mounted at a specific path
type explicitMount struct {
	mountPath      string
	configMapRef   *kubetaskv1alpha1.ConfigMapKeySelector
	secretRef      *kubetaskv1alpha1.SecretKeySelector
	inlineContent  *string
	inlineFileName string
}

// aggregatedContext holds content and metadata for a context to be aggregated
type aggregatedContext struct {
	content     string
	name        string
	contextType string
	sourceType  string
}

// processContexts processes all contexts and returns:
// - ConfigMap for aggregated content (contexts without mountPath)
// - List of explicit mounts (contexts with mountPath)
func (r *TaskReconciler) processContexts(ctx context.Context, task *kubetaskv1alpha1.Task, contexts []kubetaskv1alpha1.Context) (*corev1.ConfigMap, []explicitMount, error) {
	var aggregatedContexts []aggregatedContext
	var explicitMounts []explicitMount

	for _, c := range contexts {
		if c.Type != kubetaskv1alpha1.ContextTypeFile || c.File == nil {
			continue
		}

		file := c.File

		if file.MountPath != nil && *file.MountPath != "" {
			// Explicit mount path specified
			mount := explicitMount{
				mountPath: *file.MountPath,
			}

			switch {
			case file.Source.Inline != nil:
				mount.inlineContent = file.Source.Inline
				mount.inlineFileName = file.Name
			case file.Source.ConfigMapKeyRef != nil:
				mount.configMapRef = file.Source.ConfigMapKeyRef
			case file.Source.SecretKeyRef != nil:
				mount.secretRef = file.Source.SecretKeyRef
			}

			explicitMounts = append(explicitMounts, mount)
		} else {
			// No mount path - aggregate into task.md
			content, err := r.resolveFileContent(ctx, task.Namespace, file)
			if err != nil {
				return nil, nil, err
			}
			if content != "" {
				// Determine source type
				sourceType := "unknown"
				switch {
				case file.Source.Inline != nil:
					sourceType = "inline"
				case file.Source.ConfigMapKeyRef != nil:
					sourceType = "configMap"
				case file.Source.SecretKeyRef != nil:
					sourceType = "secret"
				}

				aggregatedContexts = append(aggregatedContexts, aggregatedContext{
					content:     content,
					name:        file.Name,
					contextType: string(c.Type),
					sourceType:  sourceType,
				})
			}
		}
	}

	// Create ConfigMap for aggregated content if any
	var configMap *corev1.ConfigMap
	if len(aggregatedContexts) > 0 {
		// Wrap each context in XML tags with metadata
		var wrappedContents []string
		for i, ac := range aggregatedContexts {
			wrapped := fmt.Sprintf("<context index=\"%d\" name=\"%s\" type=\"%s\" source=\"%s\">\n%s\n</context>",
				i, ac.name, ac.contextType, ac.sourceType, ac.content)
			wrappedContents = append(wrappedContents, wrapped)
		}
		aggregated := strings.Join(wrappedContents, "\n\n")
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
			Data: map[string]string{
				"task.md": aggregated,
			},
		}
	}

	return configMap, explicitMounts, nil
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
func (r *TaskReconciler) buildJob(task *kubetaskv1alpha1.Task, jobName string, wsConfig workspaceConfig, contextConfigMap *corev1.ConfigMap, explicitMounts []explicitMount) *batchv1.Job {
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
	if wsConfig.toolsImage != "" {
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
			Image:   wsConfig.toolsImage,
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
	for i, cred := range wsConfig.credentials {
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

	// Add aggregated context volume if ConfigMap exists
	if contextConfigMap != nil {
		volumes = append(volumes, corev1.Volume{
			Name: "aggregated-context",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: contextConfigMap.Name,
					},
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "aggregated-context",
			MountPath: AggregatedContextPath,
			SubPath:   "task.md",
		})
	}

	// Add explicit mount volumes
	for i, mount := range explicitMounts {
		volumeName := fmt.Sprintf("explicit-mount-%d", i)

		switch {
		case mount.configMapRef != nil:
			// Mount from ConfigMap
			volumes = append(volumes, corev1.Volume{
				Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: mount.configMapRef.Name,
						},
					},
				},
			})
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      volumeName,
				MountPath: mount.mountPath,
				SubPath:   mount.configMapRef.Key,
			})
		case mount.secretRef != nil:
			// Mount from Secret
			volumes = append(volumes, corev1.Volume{
				Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: mount.secretRef.Name,
					},
				},
			})
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      volumeName,
				MountPath: mount.mountPath,
				SubPath:   mount.secretRef.Key,
			})
		case mount.inlineContent != nil:
			// For inline content with explicit mountPath, we create a separate ConfigMap
			inlineConfigMapName := fmt.Sprintf("%s-inline-%d", task.Name, i)
			volumes = append(volumes, corev1.Volume{
				Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: inlineConfigMapName,
						},
					},
				},
			})
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      volumeName,
				MountPath: mount.mountPath,
				SubPath:   mount.inlineFileName,
			})
		}
	}

	// Build pod labels - start with base labels
	podLabels := map[string]string{
		"app":              "kubetask",
		"kubetask.io/task": task.Name,
	}

	// Add custom pod labels from WorkspaceConfig
	for k, v := range wsConfig.podLabels {
		podLabels[k] = v
	}

	// Build PodSpec with scheduling configuration
	podSpec := corev1.PodSpec{
		ServiceAccountName: "kubetask-agent",
		InitContainers:     initContainers,
		Containers: []corev1.Container{
			{
				Name:            "agent",
				Image:           wsConfig.agentImage,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env:             envVars,
				VolumeMounts:    volumeMounts,
			},
		},
		Volumes:       volumes,
		RestartPolicy: corev1.RestartPolicyNever,
	}

	// Apply scheduling configuration if specified
	if wsConfig.scheduling != nil {
		if wsConfig.scheduling.NodeSelector != nil {
			podSpec.NodeSelector = wsConfig.scheduling.NodeSelector
		}
		if wsConfig.scheduling.Tolerations != nil {
			podSpec.Tolerations = wsConfig.scheduling.Tolerations
		}
		if wsConfig.scheduling.Affinity != nil {
			podSpec.Affinity = wsConfig.scheduling.Affinity
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
