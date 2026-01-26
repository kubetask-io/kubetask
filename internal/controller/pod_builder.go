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
	attachImage        string   // Lightweight image for Server-mode --attach Pods
	command            []string // Command for agent container (optional, has default)
	workspaceDir       string
	contexts           []kubeopenv1alpha1.ContextItem
	config             *string // OpenCode config JSON string
	credentials        []kubeopenv1alpha1.Credential
	podSpec            *kubeopenv1alpha1.AgentPodSpec
	serviceAccountName string
	maxConcurrentTasks *int32
	quota              *kubeopenv1alpha1.QuotaConfig
	serverConfig       *kubeopenv1alpha1.ServerConfig // Server mode configuration (nil = Pod mode)
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

	// OpenCodeConfigContentEnvVar is the environment variable for injecting config content
	// This is used to inject instructions for loading context files without conflicting
	// with repository's AGENTS.md. OpenCode merges OPENCODE_CONFIG_CONTENT with OPENCODE_CONFIG.
	OpenCodeConfigContentEnvVar = "OPENCODE_CONFIG_CONTENT"

	// OpenCodePermissionEnvVar is the environment variable for OpenCode permission configuration.
	// This allows overriding permission settings to enable non-interactive/automated mode.
	// The value is a JSON object mapping tool names to permission actions (allow/ask/deny).
	OpenCodePermissionEnvVar = "OPENCODE_PERMISSION"

	// DefaultOpenCodePermission is the default permission configuration for automated execution.
	// In Kubernetes/CI environments, we need to allow all permissions to avoid interactive prompts
	// that would block task execution. Users can still restrict permissions via Agent.spec.config.
	//
	// The value must be valid JSON since OpenCode parses it with JSON.parse().
	// {"*":"allow"} sets all tools to "allow" mode, enabling full autonomous operation.
	// For restricted permissions, users should configure them in Agent.spec.config's permission field.
	DefaultOpenCodePermission = `{"*":"allow"}`

	// ContextFileRelPath is the relative path (from workspaceDir) for KubeOpenCode context file.
	// This path is chosen to avoid conflicts with repository's AGENTS.md or CLAUDE.md files.
	// OpenCode loads this file via the instructions config injected through OPENCODE_CONFIG_CONTENT.
	ContextFileRelPath = ".kubeopencode/context.md"

	// DefaultSecretFileMode is the default permission mode for mounted secrets.
	// 0600 gives read/write access to the owner only.
	DefaultSecretFileMode int32 = 0600

	// DefaultGitRef is the default Git reference to clone
	DefaultGitRef = "HEAD"

	// DefaultGitDepth is the default Git clone depth
	DefaultGitDepth = 1

	// DefaultGitRoot is the root directory for Git clones in init containers
	DefaultGitRoot = "/git"

	// DefaultGitLink is the default subdirectory name for Git clones
	DefaultGitLink = "repo"

	// DefaultHomeDir is the default HOME directory for SCC compatibility
	DefaultHomeDir = "/tmp"

	// DefaultShell is the default SHELL for SCC compatibility
	DefaultShell = "/bin/bash"
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
		depth = DefaultGitDepth
	}

	// Set default ref to HEAD if not specified
	ref := gm.ref
	if ref == "" {
		ref = DefaultGitRef
	}

	envVars := []corev1.EnvVar{
		{Name: "GIT_REPO", Value: gm.repository},
		{Name: "GIT_REF", Value: ref},
		{Name: "GIT_DEPTH", Value: strconv.Itoa(depth)},
		{Name: "GIT_ROOT", Value: DefaultGitRoot},
		{Name: "GIT_LINK", Value: DefaultGitLink},
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: volumeName, MountPath: DefaultGitRoot},
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

// buildPod creates a Pod object for the task with context mounts.
// The agentNamespace parameter specifies where the Pod will be created (may differ from Task namespace
// when using cross-namespace Agent reference).
// The serverURL parameter is used for Server-mode Agents: when non-empty, the Pod will use
// `opencode run --attach <serverURL>` to connect to an existing OpenCode server instead of
// running a standalone instance.
func buildPod(task *kubeopenv1alpha1.Task, podName string, agentNamespace string, cfg agentConfig, contextConfigMap *corev1.ConfigMap, fileMounts []fileMount, dirMounts []dirMount, gitMounts []gitMount, sysCfg systemConfig, serverURL string) *corev1.Pod {
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
	// - SHELL=/sbin/nologin - terminals in interactive tools fail to start
	// Setting these explicitly ensures containers work regardless of UID.
	envVars = append(envVars,
		corev1.EnvVar{Name: "HOME", Value: DefaultHomeDir},
		corev1.EnvVar{Name: "SHELL", Value: DefaultShell},
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

	// Set OPENCODE_PERMISSION to enable all permissions by default.
	// This is required for non-interactive/automated execution in Kubernetes.
	// Without this, OpenCode would prompt for permission approval which would
	// block task execution in a Pod environment.
	// Users can still configure restrictive permissions via Agent.spec.config's permission field,
	// which takes precedence over this environment variable.
	envVars = append(envVars, corev1.EnvVar{
		Name:  OpenCodePermissionEnvVar,
		Value: DefaultOpenCodePermission,
	})

	// Check if context file is being mounted and inject OPENCODE_CONFIG_CONTENT.
	// This allows OpenCode to load KubeOpenCode's context file without conflicting
	// with repository's AGENTS.md. The context file path is relative to workspaceDir.
	contextFilePath := cfg.workspaceDir + "/" + ContextFileRelPath
	for _, fm := range fileMounts {
		if fm.filePath == contextFilePath {
			// Inject instructions to load the context file
			// OpenCode will merge this with OPENCODE_CONFIG (if set)
			envVars = append(envVars, corev1.EnvVar{
				Name:  OpenCodeConfigContentEnvVar,
				Value: `{"instructions":["` + ContextFileRelPath + `"]}`,
			})
			break
		}
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
				var fileMode = DefaultSecretFileMode
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
			var fileMode = DefaultSecretFileMode
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
		// Start with contextInitMounts (ConfigMap volume mounts) and add workspace mount
		contextInit.VolumeMounts = contextInitMounts
		contextInit.VolumeMounts = append(contextInit.VolumeMounts, corev1.VolumeMount{
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
		subPath := DefaultGitLink
		if gm.repoPath != "" {
			subPath = DefaultGitLink + "/" + strings.TrimPrefix(gm.repoPath, "/")
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
			Value: DefaultGitRoot + "/.gitconfig",
		})
		// Mount the git volume root to access the .gitconfig
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "git-context-0",
			MountPath: DefaultGitRoot + "/.gitconfig",
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
		if serverURL != "" {
			// Server mode: use --attach flag to connect to existing OpenCode server
			// This allows Tasks to reuse a persistent server for faster execution
			agentCommand = []string{
				"sh", "-c",
				fmt.Sprintf(`/tools/opencode run --attach %s "$(cat %s/task.md)"`, serverURL, cfg.workspaceDir),
			}
		} else {
			// Pod mode: run standalone OpenCode instance
			agentCommand = []string{
				"sh", "-c",
				fmt.Sprintf(`/tools/opencode run "$(cat %s/task.md)"`, cfg.workspaceDir),
			}
		}
	}
	// Determine executor image: use lightweight attach image for Server mode
	executorImage := cfg.executorImage
	if serverURL != "" && cfg.attachImage != "" {
		// Server mode: use lightweight attach image (~25MB) instead of devbox (~1GB)
		// The attach image only needs the OpenCode binary since actual execution
		// happens in the persistent server's environment
		executorImage = cfg.attachImage
	}

	agentContainer := corev1.Container{
		Name:            "agent",
		Image:           executorImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         agentCommand,
		Env:             envVars,
		EnvFrom:         envFromSources,
		VolumeMounts:    volumeMounts,
	}

	// Build containers list
	containers := []corev1.Container{agentContainer}

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

		// Apply resource requirements if specified
		if cfg.podSpec.Resources != nil {
			podSpec.Containers[0].Resources = *cfg.podSpec.Resources
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

	// Pod cleanup is handled uniformly via finalizer on the Task.
	// We don't use OwnerReference to keep cleanup behavior consistent
	// across both same-namespace and cross-namespace scenarios.

	return pod
}
