// Copyright Contributors to the KubeTask project

package controller

import (
	"fmt"
	"strconv"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
)

// agentConfig holds the resolved configuration from Agent
type agentConfig struct {
	agentImage         string
	command            []string
	workspaceDir       string
	contexts           []kubetaskv1alpha1.ContextMount
	credentials        []kubetaskv1alpha1.Credential
	podSpec            *kubetaskv1alpha1.AgentPodSpec
	serviceAccountName string
	maxConcurrentTasks *int32
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

// gitMount represents a Git repository to be cloned and mounted
type gitMount struct {
	contextName string // Context name (for volume naming)
	repository  string // Git repository URL
	ref         string // Git reference (branch, tag, or commit SHA)
	repoPath    string // Path within the repository to mount
	mountPath   string // Where to mount in the container
	depth       int    // Clone depth (1 = shallow, 0 = full)
	secretName  string // Optional secret name for authentication
}

// resolvedContext holds a resolved context with its content and metadata
type resolvedContext struct {
	name      string // Context name (for XML tag)
	namespace string // Context namespace (for XML tag)
	ctxType   string // Context type (for XML tag)
	content   string // Resolved content
	mountPath string // Mount path (empty = append to task.md)
}

// sanitizeConfigMapKey converts a file path to a valid ConfigMap key.
// ConfigMap keys must be alphanumeric, '-', '_', or '.'.
func sanitizeConfigMapKey(filePath string) string {
	// Remove leading slash and replace remaining slashes with dashes
	key := strings.TrimPrefix(filePath, "/")
	key = strings.ReplaceAll(key, "/", "-")
	return key
}

// boolPtr returns a pointer to the given bool value
func boolPtr(b bool) *bool {
	return &b
}

const (
	// DefaultGitSyncImage is the default git-sync container image
	DefaultGitSyncImage = "registry.k8s.io/git-sync/git-sync:v4.4.0"
)

// buildGitSyncInitContainer creates an init container that clones a Git repository using git-sync.
func buildGitSyncInitContainer(gm gitMount, volumeName string, index int) corev1.Container {
	// Set default depth to 1 (shallow clone) if not specified
	depth := gm.depth
	if depth <= 0 {
		depth = 1
	}

	// Set default ref to HEAD if not specified
	ref := gm.ref
	if ref == "" {
		ref = "HEAD"
	}

	envVars := []corev1.EnvVar{
		{Name: "GITSYNC_REPO", Value: gm.repository},
		{Name: "GITSYNC_REF", Value: ref},
		{Name: "GITSYNC_ONE_TIME", Value: "true"},
		{Name: "GITSYNC_DEPTH", Value: strconv.Itoa(depth)},
		{Name: "GITSYNC_ROOT", Value: "/git"},
		{Name: "GITSYNC_LINK", Value: "repo"},
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: volumeName, MountPath: "/git"},
	}

	// Add secret volume mount for authentication if specified
	if gm.secretName != "" {
		// Mount the secret and configure git-sync to use it
		// git-sync supports GITSYNC_USERNAME/GITSYNC_PASSWORD for HTTPS
		// and GITSYNC_SSH_KEY_FILE for SSH
		envVars = append(envVars,
			corev1.EnvVar{
				Name: "GITSYNC_USERNAME",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: gm.secretName},
						Key:                  "username",
						Optional:             boolPtr(true),
					},
				},
			},
			corev1.EnvVar{
				Name: "GITSYNC_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: gm.secretName},
						Key:                  "password",
						Optional:             boolPtr(true),
					},
				},
			},
		)
	}

	return corev1.Container{
		Name:            fmt.Sprintf("git-sync-%d", index),
		Image:           DefaultGitSyncImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             envVars,
		VolumeMounts:    volumeMounts,
	}
}

// buildJob creates a Job object for the task with context mounts
func buildJob(task *kubetaskv1alpha1.Task, jobName string, cfg agentConfig, contextConfigMap *corev1.ConfigMap, fileMounts []fileMount, dirMounts []dirMount, gitMounts []gitMount) *batchv1.Job {
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount
	var envVars []corev1.EnvVar
	var initContainers []corev1.Container

	// Base environment variables
	envVars = append(envVars,
		corev1.EnvVar{Name: "TASK_NAME", Value: task.Name},
		corev1.EnvVar{Name: "TASK_NAMESPACE", Value: task.Namespace},
		corev1.EnvVar{Name: "WORKSPACE_DIR", Value: cfg.workspaceDir},
	)

	// Add human-in-the-loop keep-alive environment variable if enabled
	if task.Spec.HumanInTheLoop != nil && task.Spec.HumanInTheLoop.Enabled {
		keepAlive := DefaultKeepAlive
		if task.Spec.HumanInTheLoop.KeepAlive != nil {
			keepAlive = task.Spec.HumanInTheLoop.KeepAlive.Duration
		}
		keepAliveSeconds := int64(keepAlive.Seconds())
		envVars = append(envVars, corev1.EnvVar{
			Name:  EnvHumanInTheLoopKeepAlive,
			Value: strconv.FormatInt(keepAliveSeconds, 10),
		})
	}

	// envFromSources collects secretRef entries for mounting entire secrets
	var envFromSources []corev1.EnvFromSource

	// Add credentials (secrets as env vars or file mounts)
	for i, cred := range cfg.credentials {
		// Check if Key is specified - determines mounting behavior
		if cred.SecretRef.Key == nil || *cred.SecretRef.Key == "" {
			// No key specified: mount entire secret
			if cred.MountPath != nil && *cred.MountPath != "" {
				// Mount entire secret as a directory (each key becomes a file)
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
							SecretName:  cred.SecretRef.Name,
							DefaultMode: &fileMode,
						},
					},
				})
				volumeMounts = append(volumeMounts, corev1.VolumeMount{
					Name:      volumeName,
					MountPath: *cred.MountPath,
				})
			} else {
				// Mount entire secret as environment variables
				envFromSources = append(envFromSources, corev1.EnvFromSource{
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: cred.SecretRef.Name,
						},
					},
				})
			}
			continue
		}

		// Key is specified: use the existing single-key mounting behavior
		// Add as environment variable if Env is specified
		if cred.Env != nil && *cred.Env != "" {
			envVars = append(envVars, corev1.EnvVar{
				Name: *cred.Env,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: cred.SecretRef.Name,
						},
						Key: *cred.SecretRef.Key,
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
								Key:  *cred.SecretRef.Key,
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

	// Add Git context mounts (using git-sync init containers)
	for i, gm := range gitMounts {
		volumeName := fmt.Sprintf("git-context-%d", i)

		// Add emptyDir volume for git content
		volumes = append(volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})

		// Build init container for git-sync
		initContainers = append(initContainers, buildGitSyncInitContainer(gm, volumeName, i))

		// Add volume mount to agent container
		// If repoPath is specified, use subPath to mount only that path
		subPath := "repo"
		if gm.repoPath != "" {
			subPath = "repo/" + strings.TrimPrefix(gm.repoPath, "/")
		}
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: gm.mountPath,
			SubPath:   subPath,
		})
	}

	// Build pod labels - start with base labels
	podLabels := map[string]string{
		"app":              "kubetask",
		"kubetask.io/task": task.Name,
	}

	// Add custom pod labels from Agent.PodSpec
	if cfg.podSpec != nil {
		for k, v := range cfg.podSpec.Labels {
			podLabels[k] = v
		}
	}

	// Build agent container
	agentContainer := corev1.Container{
		Name:            "agent",
		Image:           cfg.agentImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             envVars,
		EnvFrom:         envFromSources,
		VolumeMounts:    volumeMounts,
	}

	// Apply command (required field in Agent spec)
	// If humanInTheLoop is enabled on the Task, wrap the command with sleep
	if task.Spec.HumanInTheLoop != nil && task.Spec.HumanInTheLoop.Enabled {
		keepAlive := DefaultKeepAlive
		if task.Spec.HumanInTheLoop.KeepAlive != nil {
			keepAlive = task.Spec.HumanInTheLoop.KeepAlive.Duration
		}
		keepAliveSeconds := int64(keepAlive.Seconds())

		// Build the wrapped command that runs original command then sleeps
		// Format: sh -c 'original_command; EXIT_CODE=$?; echo "Human-in-the-loop: keeping container alive..."; sleep N; exit $EXIT_CODE'
		originalCmd := strings.Join(cfg.command, " ")
		wrappedScript := fmt.Sprintf(
			`%s; EXIT_CODE=$?; echo "Human-in-the-loop: keeping container alive for %d seconds. Use 'kubectl exec' to access."; sleep %d; exit $EXIT_CODE`,
			originalCmd, keepAliveSeconds, keepAliveSeconds,
		)
		agentContainer.Command = []string{"sh", "-c", wrappedScript}
	} else {
		// No humanInTheLoop on Task, use command as-is
		agentContainer.Command = cfg.command
	}

	// Build PodSpec with scheduling configuration
	podSpec := corev1.PodSpec{
		ServiceAccountName: cfg.serviceAccountName,
		InitContainers:     initContainers,
		Containers:         []corev1.Container{agentContainer},
		Volumes:            volumes,
		RestartPolicy:      corev1.RestartPolicyNever,
	}

	// Apply PodSpec configuration if specified
	if cfg.podSpec != nil {
		// Apply scheduling configuration
		if cfg.podSpec.Scheduling != nil {
			if cfg.podSpec.Scheduling.NodeSelector != nil {
				podSpec.NodeSelector = cfg.podSpec.Scheduling.NodeSelector
			}
			if cfg.podSpec.Scheduling.Tolerations != nil {
				podSpec.Tolerations = cfg.podSpec.Scheduling.Tolerations
			}
			if cfg.podSpec.Scheduling.Affinity != nil {
				podSpec.Affinity = cfg.podSpec.Scheduling.Affinity
			}
		}

		// Apply runtime class if specified (for gVisor, Kata, etc.)
		if cfg.podSpec.RuntimeClassName != nil {
			podSpec.RuntimeClassName = cfg.podSpec.RuntimeClassName
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
