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
	contexts           []kubetaskv1alpha1.ContextSource
	credentials        []kubetaskv1alpha1.Credential
	podSpec            *kubetaskv1alpha1.AgentPodSpec
	serviceAccountName string
	maxConcurrentTasks *int32
	humanInTheLoop     *kubetaskv1alpha1.HumanInTheLoop
}

// sessionPVCConfig holds PVC configuration for session persistence.
// Whether persistence is enabled is determined by the Agent's humanInTheLoop.persistence.enabled field.
type sessionPVCConfig struct {
	pvcName string
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

// int32Ptr returns a pointer to the given int32 value
func int32Ptr(i int32) *int32 {
	return &i
}

const (
	// DefaultToolsImage is the default kubetask-tools container image for infrastructure utilities.
	// This image provides: git-init (Git clone), save-session (workspace persistence), etc.
	DefaultToolsImage = "quay.io/kubetask/kubetask-tools:latest"
)

// buildGitInitContainer creates an init container that clones a Git repository.
func buildGitInitContainer(gm gitMount, volumeName string, index int) corev1.Container {
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
		{Name: "GIT_REPO", Value: gm.repository},
		{Name: "GIT_REF", Value: ref},
		{Name: "GIT_DEPTH", Value: strconv.Itoa(depth)},
		{Name: "GIT_ROOT", Value: "/git"},
		{Name: "GIT_LINK", Value: "repo"},
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: volumeName, MountPath: "/git"},
	}

	// Add secret environment variables for authentication if specified
	if gm.secretName != "" {
		// git-init supports GIT_USERNAME/GIT_PASSWORD for HTTPS
		envVars = append(envVars,
			corev1.EnvVar{
				Name: "GIT_USERNAME",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: gm.secretName},
						Key:                  "username",
						Optional:             boolPtr(true),
					},
				},
			},
			corev1.EnvVar{
				Name: "GIT_PASSWORD",
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
		Name:            fmt.Sprintf("git-init-%d", index),
		Image:           DefaultToolsImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/kubetask-tools", "git-init"},
		Env:             envVars,
		VolumeMounts:    volumeMounts,
	}
}

// buildJob creates a Job object for the task with context mounts
func buildJob(task *kubetaskv1alpha1.Task, jobName string, cfg agentConfig, contextConfigMap *corev1.ConfigMap, fileMounts []fileMount, dirMounts []dirMount, gitMounts []gitMount, sessionCfg *sessionPVCConfig) *batchv1.Job {
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

	// Get humanInTheLoop configuration from Agent only
	// (Task-level humanInTheLoop was removed to simplify the API)
	effectiveHumanInTheLoop := cfg.humanInTheLoop

	// Add human-in-the-loop session duration environment variable if sidecar is enabled
	sidecarEnabled := effectiveHumanInTheLoop != nil &&
		effectiveHumanInTheLoop.Sidecar != nil && effectiveHumanInTheLoop.Sidecar.Enabled
	if sidecarEnabled {
		sessionDuration := DefaultSessionDuration
		if effectiveHumanInTheLoop.Sidecar.Duration != nil {
			sessionDuration = effectiveHumanInTheLoop.Sidecar.Duration.Duration
		}
		durationSeconds := int64(sessionDuration.Seconds())
		envVars = append(envVars, corev1.EnvVar{
			Name:  EnvHumanInTheLoopDuration,
			Value: strconv.FormatInt(durationSeconds, 10),
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

	// Add Git context mounts (using git-init containers)
	for i, gm := range gitMounts {
		volumeName := fmt.Sprintf("git-context-%d", i)

		// Add emptyDir volume for git content
		volumes = append(volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})

		// Build init container for git clone
		initContainers = append(initContainers, buildGitInitContainer(gm, volumeName, i))

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

	// If we have Git mounts, add GIT_CONFIG_GLOBAL to point to shared gitconfig
	// This is needed because init containers run as different users and git will
	// refuse to work without safe.directory configured
	if len(gitMounts) > 0 {
		// The first git-init container writes .gitconfig to /git/.gitconfig
		// which is shared via git-context-0 volume
		envVars = append(envVars, corev1.EnvVar{
			Name:  "GIT_CONFIG_GLOBAL",
			Value: "/git/.gitconfig",
		})
		// Mount the git volume root to access the .gitconfig
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "git-context-0",
			MountPath: "/git/.gitconfig",
			SubPath:   ".gitconfig",
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

	// Apply command as-is (no wrapping needed - humanInTheLoop uses sidecar approach)
	agentContainer.Command = cfg.command

	// Build containers list - start with agent container
	containers := []corev1.Container{agentContainer}

	// Add session sidecar container if sidecar is enabled
	if sidecarEnabled {
		// Determine sidecar image: use custom image if specified, otherwise use agentImage
		sidecarImage := cfg.agentImage
		if effectiveHumanInTheLoop.Image != "" {
			sidecarImage = effectiveHumanInTheLoop.Image
		}

		// Determine sidecar command: use Command if specified, otherwise use sleep with Duration
		var sidecarCommand []string
		if len(effectiveHumanInTheLoop.Command) > 0 {
			// Use custom command (shared by both sidecar and resumed session)
			sidecarCommand = effectiveHumanInTheLoop.Command
		} else {
			// Use sleep with session duration (default: 1h)
			sessionDuration := DefaultSessionDuration
			if effectiveHumanInTheLoop.Sidecar.Duration != nil {
				sessionDuration = effectiveHumanInTheLoop.Sidecar.Duration.Duration
			}
			durationSeconds := int64(sessionDuration.Seconds())
			sidecarCommand = []string{"sleep", fmt.Sprintf("%d", durationSeconds)}
		}

		// Build session sidecar container with same mounts and env as agent
		sidecar := corev1.Container{
			Name:            "session",
			Image:           sidecarImage,
			ImagePullPolicy: agentContainer.ImagePullPolicy,
			Command:         sidecarCommand,
			WorkingDir:      cfg.workspaceDir,
			Env:             agentContainer.Env,
			EnvFrom:         agentContainer.EnvFrom,
			VolumeMounts:    agentContainer.VolumeMounts,
		}

		// Apply container ports to sidecar (for port-forwarding)
		if len(effectiveHumanInTheLoop.Ports) > 0 {
			var containerPorts []corev1.ContainerPort
			for _, port := range effectiveHumanInTheLoop.Ports {
				protocol := port.Protocol
				if protocol == "" {
					protocol = corev1.ProtocolTCP
				}
				containerPorts = append(containerPorts, corev1.ContainerPort{
					Name:          port.Name,
					ContainerPort: port.ContainerPort,
					Protocol:      protocol,
				})
			}
			sidecar.Ports = containerPorts
		}

		containers = append(containers, sidecar)
	}

	// Add save-session sidecar if session persistence is enabled
	// This sidecar waits for the agent to complete and copies workspace to PVC
	// Persistence is enabled when:
	// 1. Agent has humanInTheLoop.persistence.enabled = true
	// 2. KubeTaskConfig has sessionPVC configured (provides PVC name)
	enableSessionPersistence := sessionCfg != nil && effectiveHumanInTheLoop != nil &&
		effectiveHumanInTheLoop.Persistence != nil && effectiveHumanInTheLoop.Persistence.Enabled
	if enableSessionPersistence {
		// Add PVC volume for session persistence
		volumes = append(volumes, corev1.Volume{
			Name: "session-pvc",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: sessionCfg.pvcName,
				},
			},
		})

		// Add shared volume for signaling between agent and save-session containers
		volumes = append(volumes, corev1.Volume{
			Name: "session-signal",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})

		// Build save-session sidecar container
		// Uses kubetask-tools save-session command to wait for agent and save workspace
		saveSessionSidecar := corev1.Container{
			Name:            "save-session",
			Image:           DefaultToolsImage,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/kubetask-tools", "save-session"},
			Env: []corev1.EnvVar{
				{Name: "TASK_NAME", Value: task.Name},
				{Name: "TASK_NAMESPACE", Value: task.Namespace},
				{Name: "WORKSPACE_DIR", Value: cfg.workspaceDir},
				{Name: "PVC_MOUNT_PATH", Value: "/pvc"},
				{Name: "SIGNAL_FILE", Value: "/signal/.agent-done"},
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "session-pvc", MountPath: "/pvc"},
				{Name: "session-signal", MountPath: "/signal"},
			},
		}
		// Add the same volume mounts as agent container for accessing workspace
		saveSessionSidecar.VolumeMounts = append(saveSessionSidecar.VolumeMounts, volumeMounts...)

		containers = append(containers, saveSessionSidecar)

		// Wrap the agent command to create signal file on exit
		// This modifies the agent container's command to signal completion
		agentContainer.VolumeMounts = append(agentContainer.VolumeMounts, corev1.VolumeMount{
			Name:      "session-signal",
			MountPath: "/signal",
		})
		// Update the agent container in the containers list
		containers[0] = agentContainer

		// Wrap the original command to create signal file on completion
		originalCmd := strings.Join(cfg.command, " ")
		wrappedCmd := fmt.Sprintf(`%s; EXIT_CODE=$?; touch /signal/.agent-done; exit $EXIT_CODE`, originalCmd)
		containers[0].Command = []string{"sh", "-c", wrappedCmd}
	}

	// Build PodSpec with scheduling configuration
	podSpec := corev1.PodSpec{
		ServiceAccountName: cfg.serviceAccountName,
		InitContainers:     initContainers,
		Containers:         containers,
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
			BackoffLimit: int32Ptr(0), // No retries - AI tasks are not idempotent
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabels,
				},
				Spec: podSpec,
			},
		},
	}
}

// buildSessionPod creates a Pod for resuming work on a completed Task.
// The session Pod has the same image, credentials, and environment as the original agent,
// but mounts the persisted workspace from the shared PVC.
func buildSessionPod(task *kubetaskv1alpha1.Task, cfg agentConfig, sessionCfg *sessionPVCConfig) *corev1.Pod {
	sessionPodName := fmt.Sprintf("%s-session", task.Name)
	pvcSubPath := fmt.Sprintf("%s/%s", task.Namespace, task.Name)

	// Base environment variables
	envVars := []corev1.EnvVar{
		{Name: "TASK_NAME", Value: task.Name},
		{Name: "TASK_NAMESPACE", Value: task.Namespace},
		{Name: "WORKSPACE_DIR", Value: cfg.workspaceDir},
	}

	// envFromSources collects secretRef entries for mounting entire secrets
	var envFromSources []corev1.EnvFromSource

	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount

	// Add session PVC volume - this is where the persisted workspace is stored
	volumes = append(volumes, corev1.Volume{
		Name: "session-workspace",
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: sessionCfg.pvcName,
			},
		},
	})

	// Mount the PVC subdirectory as the workspace
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "session-workspace",
		MountPath: cfg.workspaceDir,
		SubPath:   pvcSubPath,
	})

	// Add credentials (same as in buildJob)
	for i, cred := range cfg.credentials {
		if cred.SecretRef.Key == nil || *cred.SecretRef.Key == "" {
			if cred.MountPath != nil && *cred.MountPath != "" {
				volumeName := fmt.Sprintf("credential-%d", i)
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

		if cred.MountPath != nil && *cred.MountPath != "" {
			volumeName := fmt.Sprintf("credential-%d", i)
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

	// Determine session command and image from humanInTheLoop config
	var command []string
	image := cfg.agentImage

	if cfg.humanInTheLoop != nil {
		// Use shared image if specified
		if cfg.humanInTheLoop.Image != "" {
			image = cfg.humanInTheLoop.Image
		}

		// Use shared command if specified
		if len(cfg.humanInTheLoop.Command) > 0 {
			command = cfg.humanInTheLoop.Command
		} else {
			// Default: sleep for the sidecar duration (if configured) or 1 hour
			sessionDuration := DefaultSessionDuration
			if cfg.humanInTheLoop.Sidecar != nil && cfg.humanInTheLoop.Sidecar.Duration != nil {
				sessionDuration = cfg.humanInTheLoop.Sidecar.Duration.Duration
			}
			durationSeconds := int64(sessionDuration.Seconds())
			command = []string{"sleep", fmt.Sprintf("%d", durationSeconds)}
		}
	} else {
		// Fallback: default sleep
		command = []string{"sleep", "3600"}
	}

	// Build session container
	sessionContainer := corev1.Container{
		Name:            "session",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         command,
		WorkingDir:      cfg.workspaceDir,
		Env:             envVars,
		EnvFrom:         envFromSources,
		VolumeMounts:    volumeMounts,
	}

	// Apply ports if specified
	if cfg.humanInTheLoop != nil && len(cfg.humanInTheLoop.Ports) > 0 {
		var containerPorts []corev1.ContainerPort
		for _, port := range cfg.humanInTheLoop.Ports {
			protocol := port.Protocol
			if protocol == "" {
				protocol = corev1.ProtocolTCP
			}
			containerPorts = append(containerPorts, corev1.ContainerPort{
				Name:          port.Name,
				ContainerPort: port.ContainerPort,
				Protocol:      protocol,
			})
		}
		sessionContainer.Ports = containerPorts
	}

	// Build pod labels
	podLabels := map[string]string{
		"app":                      "kubetask",
		"kubetask.io/task":         task.Name,
		"kubetask.io/session-task": task.Name,
		"kubetask.io/component":    "session",
	}

	// Build PodSpec
	podSpec := corev1.PodSpec{
		ServiceAccountName: cfg.serviceAccountName,
		Containers:         []corev1.Container{sessionContainer},
		Volumes:            volumes,
		RestartPolicy:      corev1.RestartPolicyNever,
	}

	// Apply PodSpec configuration if specified
	if cfg.podSpec != nil {
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
		if cfg.podSpec.RuntimeClassName != nil {
			podSpec.RuntimeClassName = cfg.podSpec.RuntimeClassName
		}
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sessionPodName,
			Namespace: task.Namespace,
			Labels:    podLabels,
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
		Spec: podSpec,
	}
}
