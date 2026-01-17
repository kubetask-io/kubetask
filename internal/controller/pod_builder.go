// Copyright Contributors to the KubeOpenCode project

package controller

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeopenv1alpha1 "github.com/kubeopencode/kubeopencode/api/v1alpha1"
)

// agentConfig holds the resolved configuration from Agent
type agentConfig struct {
	agentImage         string   // OpenCode init container image (copies binary to /tools)
	executorImage      string   // Worker container image for task execution
	command            []string // Command for agent container (optional, has default)
	workspaceDir       string
	contexts           []kubeopenv1alpha1.ContextItem
	config             *string // OpenCode config JSON string
	credentials        []kubeopenv1alpha1.Credential
	podSpec            *kubeopenv1alpha1.AgentPodSpec
	serviceAccountName string
	maxConcurrentTasks *int32
	quota              *kubeopenv1alpha1.QuotaConfig
}

// systemConfig holds resolved system-level configuration from KubeOpenCodeConfig.
// This configures internal KubeOpenCode components (git-init, context-init).
type systemConfig struct {
	// systemImage is the container image for internal KubeOpenCode components.
	// Defaults to DefaultKubeOpenCodeImage if not specified.
	systemImage string
	// systemImagePullPolicy is the image pull policy for system containers.
	// Defaults to IfNotPresent if not specified.
	systemImagePullPolicy corev1.PullPolicy
}

// fileMount represents a file to be mounted at a specific path
type fileMount struct {
	filePath string
	fileMode *int32 // Optional file permission mode (e.g., 0755 for executable)
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
	fileMode  *int32 // Optional file permission mode (e.g., 0755 for executable)
}

// sanitizeConfigMapKey converts a file path to a valid ConfigMap key.
// ConfigMap keys must be alphanumeric, '-', '_', or '.'.
func sanitizeConfigMapKey(filePath string) string {
	// Remove leading slash and replace remaining slashes with dashes
	key := strings.TrimPrefix(filePath, "/")
	key = strings.ReplaceAll(key, "/", "-")
	return key
}

// getParentDir returns the parent directory of a file path.
// For "/etc/github-app/script.sh", it returns "/etc/github-app".
func getParentDir(filePath string) string {
	lastSlash := strings.LastIndex(filePath, "/")
	if lastSlash <= 0 {
		return "/"
	}
	return filePath[:lastSlash]
}

// isUnderPath checks if filePath is under basePath.
// For example, "/workspace/task.md" is under "/workspace".
func isUnderPath(filePath, basePath string) bool {
	// Normalize paths to ensure consistent comparison
	basePath = strings.TrimSuffix(basePath, "/")
	return filePath == basePath || strings.HasPrefix(filePath, basePath+"/")
}

// sanitizeVolumeName converts a directory path to a valid Kubernetes volume name.
// Volume names must be lowercase alphanumeric, '-', '.', max 63 chars.
func sanitizeVolumeName(dirPath string) string {
	// Remove leading slash and replace slashes with dashes
	name := strings.TrimPrefix(dirPath, "/")
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ToLower(name)
	// Prepend "ctx-" to make it clear this is a context volume
	name = "ctx-" + name
	// Truncate to 63 chars max
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

// boolPtr returns a pointer to the given bool value
func boolPtr(b bool) *bool {
	return &b
}

const (
	// DefaultKubeOpenCodeImage is the default kubeopencode container image.
	// This unified image provides: controller, git-init (Git clone), etc.
	DefaultKubeOpenCodeImage = "quay.io/kubeopencode/kubeopencode:latest"

	// ToolsVolumeName is the volume name for sharing OpenCode binary between containers
	ToolsVolumeName = "tools"

	// ToolsMountPath is the mount path for the tools volume
	ToolsMountPath = "/tools"

	// OpenCodeConfigPath is the path where OpenCode config is written
	OpenCodeConfigPath = "/tools/opencode.json"

	// OpenCodeConfigEnvVar is the environment variable name for OpenCode config path
	OpenCodeConfigEnvVar = "OPENCODE_CONFIG"
)

// buildOpenCodeInitContainer creates an init container that copies OpenCode binary to /tools.
// This enables the two-container pattern where:
// - Init container (agentImage): Contains OpenCode, copies it to /tools
// - Worker container (executorImage): Uses /tools/opencode to execute tasks
func buildOpenCodeInitContainer(agentImage string) corev1.Container {
	return corev1.Container{
		Name:            "opencode-init",
		Image:           agentImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		// Uses default entrypoint from agents/opencode/entrypoint.sh
		// which copies /opencode to ${TOOLS_DIR}/opencode
		Env: []corev1.EnvVar{
			{Name: "TOOLS_DIR", Value: ToolsMountPath},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: ToolsVolumeName, MountPath: ToolsMountPath},
		},
	}
}

// buildGitInitContainer creates an init container that clones a Git repository.
func buildGitInitContainer(gm gitMount, volumeName string, index int, sysCfg systemConfig) corev1.Container {
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
		Image:           sysCfg.systemImage,
		ImagePullPolicy: sysCfg.systemImagePullPolicy,
		Command:         []string{"/kubeopencode", "git-init"},
		Env:             envVars,
		VolumeMounts:    volumeMounts,
	}
}

// contextInitFileMapping represents a mapping from ConfigMap key to target file path.
// This mirrors the FileMapping struct in cmd/kubeopencode/context_init.go.
type contextInitFileMapping struct {
	Key        string `json:"key"`
	TargetPath string `json:"targetPath"`
	FileMode   *int32 `json:"fileMode,omitempty"` // Optional file permission mode (e.g., 0755)
}

// contextInitDirMapping represents a mapping from source directory to target directory.
// This mirrors the DirMapping struct in cmd/kubeopencode/context_init.go.
type contextInitDirMapping struct {
	SourcePath string `json:"sourcePath"`
	TargetPath string `json:"targetPath"`
}

// buildContextInitContainer creates an init container that copies ConfigMap content to the writable workspace.
// This enables agents to create files in the workspace directory, which is not possible with direct ConfigMap mounts.
// The init container uses /kubeopencode context-init command which reads configuration from environment variables.
func buildContextInitContainer(workspaceDir string, fileMounts []fileMount, dirMounts []dirMount, sysCfg systemConfig) corev1.Container {
	envVars := []corev1.EnvVar{
		{Name: "WORKSPACE_DIR", Value: workspaceDir},
		{Name: "CONFIGMAP_PATH", Value: "/configmap-files"},
	}

	// Build file mappings JSON
	if len(fileMounts) > 0 {
		var mappings []contextInitFileMapping
		for _, mount := range fileMounts {
			mappings = append(mappings, contextInitFileMapping{
				Key:        sanitizeConfigMapKey(mount.filePath),
				TargetPath: mount.filePath,
				FileMode:   mount.fileMode,
			})
		}
		mappingsJSON, _ := json.Marshal(mappings)
		envVars = append(envVars, corev1.EnvVar{
			Name:  "FILE_MAPPINGS",
			Value: string(mappingsJSON),
		})
	}

	// Build directory mappings JSON
	if len(dirMounts) > 0 {
		var mappings []contextInitDirMapping
		for i, dm := range dirMounts {
			mappings = append(mappings, contextInitDirMapping{
				SourcePath: fmt.Sprintf("/configmap-dir-%d", i),
				TargetPath: dm.dirPath,
			})
		}
		mappingsJSON, _ := json.Marshal(mappings)
		envVars = append(envVars, corev1.EnvVar{
			Name:  "DIR_MAPPINGS",
			Value: string(mappingsJSON),
		})
	}

	return corev1.Container{
		Name:            "context-init",
		Image:           sysCfg.systemImage,
		ImagePullPolicy: sysCfg.systemImagePullPolicy,
		Command:         []string{"/kubeopencode", "context-init"},
		Env:             envVars,
		// VolumeMounts will be added by the caller
	}
}

// buildOutputCollectorSidecar creates a sidecar container that collects outputs from files.
// The sidecar waits for the executor container to exit, reads files specified in the merged
// OutputSpec, and writes the collected parameters to /dev/termination-log.
// Returns nil if mergedOutputs is nil or has no parameters.
func buildOutputCollectorSidecar(mergedOutputs *kubeopenv1alpha1.OutputSpec, workspaceDir string, sysCfg systemConfig) *corev1.Container {
	if mergedOutputs == nil || len(mergedOutputs.Parameters) == 0 {
		return nil // No outputs defined, skip sidecar
	}

	// Serialize merged output spec to JSON for sidecar to read
	outputSpecJSON, _ := json.Marshal(mergedOutputs)

	return &corev1.Container{
		Name:            "output-collector",
		Image:           sysCfg.systemImage,
		ImagePullPolicy: sysCfg.systemImagePullPolicy,
		Command:         []string{"/kubeopencode", "collect-outputs"},
		Env: []corev1.EnvVar{
			{Name: "WORKSPACE_DIR", Value: workspaceDir},
			{Name: "OUTPUT_SPEC", Value: string(outputSpecJSON)},
		},
		// VolumeMounts will be added by the caller (workspace volume)
	}
}

// buildPod creates a Pod object for the task with context mounts and optional output collector sidecar.
// If mergedOutputs is non-nil, a sidecar container is added to collect outputs from files.
// The agentNamespace parameter specifies where the Pod will be created (may differ from Task namespace
// when using cross-namespace Agent reference).
func buildPod(task *kubeopenv1alpha1.Task, podName string, agentNamespace string, cfg agentConfig, contextConfigMap *corev1.ConfigMap, fileMounts []fileMount, dirMounts []dirMount, gitMounts []gitMount, mergedOutputs *kubeopenv1alpha1.OutputSpec, sysCfg systemConfig) *corev1.Pod {
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount
	var envVars []corev1.EnvVar
	var initContainers []corev1.Container

	// Add tools volume for sharing OpenCode binary between init and worker containers.
	// The OpenCode init container copies the binary to /tools, and the worker container uses it.
	volumes = append(volumes, corev1.Volume{
		Name: ToolsVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      ToolsVolumeName,
		MountPath: ToolsMountPath,
	})

	// Add OpenCode init container FIRST - it copies the OpenCode binary to /tools
	initContainers = append(initContainers, buildOpenCodeInitContainer(cfg.agentImage))

	// Always add workspace emptyDir volume for writable workspace.
	// This is essential for SCC environments where containers run with random UIDs
	// that don't have write access to directories created in the container image.
	volumes = append(volumes, corev1.Volume{
		Name: "workspace",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "workspace",
		MountPath: cfg.workspaceDir,
	})

	// Base environment variables for SCC (Security Context Constraints) compatibility.
	// In environments with SCC or similar security policies, containers run with
	// random UIDs that have no /etc/passwd entry, causing:
	// - HOME=/ (not writable) - tools like gemini-cli fail to create ~/.gemini
	// - SHELL=/sbin/nologin - terminals in code-server fail to start
	// Setting these explicitly ensures containers work regardless of UID.
	envVars = append(envVars,
		corev1.EnvVar{Name: "HOME", Value: "/tmp"},
		corev1.EnvVar{Name: "SHELL", Value: "/bin/bash"},
		corev1.EnvVar{Name: "TASK_NAME", Value: task.Name},
		corev1.EnvVar{Name: "TASK_NAMESPACE", Value: task.Namespace},
		corev1.EnvVar{Name: "WORKSPACE_DIR", Value: cfg.workspaceDir},
	)

	// If OpenCode config is provided, set OPENCODE_CONFIG env var
	if cfg.config != nil && *cfg.config != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  OpenCodeConfigEnvVar,
			Value: OpenCodeConfigPath,
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

	// Track volume mounts for the context-init container
	var contextInitMounts []corev1.VolumeMount

	// Add context ConfigMap volume if it exists (for aggregated content)
	// The ConfigMap is mounted to the init container, which copies content to the writable workspace
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

		// Mount ConfigMap to init container at a temporary path
		contextInitMounts = append(contextInitMounts, corev1.VolumeMount{
			Name:      "context-files",
			MountPath: "/configmap-files",
			ReadOnly:  true,
		})
	}

	// Add directory mounts (ConfigMapRef - entire ConfigMap as a directory)
	// These are also mounted to the init container and copied to workspace
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

		// Mount ConfigMap to init container at a temporary path
		contextInitMounts = append(contextInitMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: fmt.Sprintf("/configmap-dir-%d", i),
			ReadOnly:  true,
		})
	}

	// Add context-init container if there are any context files or directories to copy
	if len(fileMounts) > 0 || len(dirMounts) > 0 {
		contextInit := buildContextInitContainer(cfg.workspaceDir, fileMounts, dirMounts, sysCfg)
		// Add workspace mount so init container can write to it
		contextInit.VolumeMounts = append(contextInitMounts, corev1.VolumeMount{
			Name:      "workspace",
			MountPath: cfg.workspaceDir,
		})

		// If OpenCode config is provided, mount /tools volume in context-init
		// so it can write the config file. The /tools volume is already created
		// for sharing the OpenCode binary between containers.
		if cfg.config != nil && *cfg.config != "" {
			contextInit.VolumeMounts = append(contextInit.VolumeMounts, corev1.VolumeMount{
				Name:      ToolsVolumeName,
				MountPath: ToolsMountPath,
			})
		}

		// For files outside /workspace, we need to create shared emptyDir volumes
		// so that the context-init container can write files that persist to the agent container.
		// Group files by their parent directory to minimize the number of volumes.
		externalDirs := make(map[string]bool)
		for _, fm := range fileMounts {
			if !isUnderPath(fm.filePath, cfg.workspaceDir) {
				parentDir := getParentDir(fm.filePath)
				// Skip /tools as it already exists for the OpenCode binary
				if parentDir == ToolsMountPath {
					continue
				}
				externalDirs[parentDir] = true
			}
		}

		// Create emptyDir volumes for each unique external parent directory
		for dir := range externalDirs {
			volumeName := sanitizeVolumeName(dir)

			// Add emptyDir volume for this external directory
			volumes = append(volumes, corev1.Volume{
				Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			})

			// Mount this volume in context-init container
			contextInit.VolumeMounts = append(contextInit.VolumeMounts, corev1.VolumeMount{
				Name:      volumeName,
				MountPath: dir,
			})

			// Mount this volume in agent container
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      volumeName,
				MountPath: dir,
			})
		}

		initContainers = append(initContainers, contextInit)
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
		initContainers = append(initContainers, buildGitInitContainer(gm, volumeName, i, sysCfg))

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
		"app":                  "kubeopencode",
		"kubeopencode.io/task": task.Name,
	}

	// Track Task namespace when Pod runs in a different namespace (cross-namespace Agent reference)
	if agentNamespace != task.Namespace {
		podLabels[TaskNamespaceLabelKey] = task.Namespace
	}

	// Add custom pod labels from Agent.PodSpec
	if cfg.podSpec != nil {
		for k, v := range cfg.podSpec.Labels {
			podLabels[k] = v
		}
	}

	// Build agent container using executorImage (the worker container)
	// The OpenCode binary is available at /tools/opencode from the init container
	// Use custom command if provided, otherwise use default
	agentCommand := cfg.command
	if len(agentCommand) == 0 {
		// Default command: /tools/opencode run "$(cat ${WORKSPACE_DIR}/task.md)"
		agentCommand = []string{
			"sh", "-c",
			fmt.Sprintf(`/tools/opencode run "$(cat %s/task.md)"`, cfg.workspaceDir),
		}
	}
	agentContainer := corev1.Container{
		Name:            "agent",
		Image:           cfg.executorImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         agentCommand,
		Env:             envVars,
		EnvFrom:         envFromSources,
		VolumeMounts:    volumeMounts,
	}

	// Build containers list
	containers := []corev1.Container{agentContainer}

	// Add output-collector sidecar if outputs are defined
	var shareProcessNamespace *bool
	if sidecar := buildOutputCollectorSidecar(mergedOutputs, cfg.workspaceDir, sysCfg); sidecar != nil {
		// Sidecar needs access to the workspace volume to read output files
		sidecar.VolumeMounts = []corev1.VolumeMount{
			{Name: "workspace", MountPath: cfg.workspaceDir},
		}
		containers = append(containers, *sidecar)
		// Enable shared PID namespace so sidecar can detect when agent container exits
		shareProcessNamespace = boolPtr(true)
	}

	// Build PodSpec with scheduling configuration
	podSpec := corev1.PodSpec{
		ServiceAccountName:    cfg.serviceAccountName,
		ShareProcessNamespace: shareProcessNamespace,
		InitContainers:        initContainers,
		Containers:            containers,
		Volumes:               volumes,
		RestartPolicy:         corev1.RestartPolicyNever,
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

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: agentNamespace, // Pod runs in Agent's namespace
			Labels:    podLabels,
		},
		Spec: podSpec,
	}

	// Only set OwnerReference when Pod is in the same namespace as Task.
	// Cross-namespace owner references are not allowed in Kubernetes.
	// For cross-namespace cleanup, the controller uses a finalizer on the Task.
	if agentNamespace == task.Namespace {
		pod.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion: task.APIVersion,
				Kind:       task.Kind,
				Name:       task.Name,
				UID:        task.UID,
				Controller: boolPtr(true),
			},
		}
	}

	return pod
}
