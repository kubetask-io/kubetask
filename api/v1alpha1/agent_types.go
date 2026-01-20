// Copyright Contributors to the KubeOpenCode project

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced",shortName=ag
// +kubebuilder:printcolumn:JSONPath=`.spec.agentImage`,name="Image",type=string,priority=1
// +kubebuilder:printcolumn:JSONPath=`.spec.serviceAccountName`,name="ServiceAccount",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// Agent defines the AI agent configuration for task execution.
// Agent = AI agent + permissions + tools + infrastructure
// This is the execution black box - Task creators don't need to understand execution details.
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the agent configuration
	Spec AgentSpec `json:"spec"`

	// Status represents the current status of the Agent
	// +optional
	Status AgentStatus `json:"status,omitempty"`
}

// QuotaConfig defines rate limiting for Task starts within a sliding time window.
// This is different from maxConcurrentTasks which limits concurrent running Tasks.
// Quota limits the RATE at which new Tasks can start.
type QuotaConfig struct {
	// MaxTaskStarts is the maximum number of Task starts allowed within the window.
	// +kubebuilder:validation:Minimum=1
	// +required
	MaxTaskStarts int32 `json:"maxTaskStarts"`

	// WindowSeconds defines the sliding window duration in seconds.
	// For example, 3600 (1 hour) means "max N tasks per hour".
	// +kubebuilder:validation:Minimum=60
	// +kubebuilder:validation:Maximum=86400
	// +required
	WindowSeconds int32 `json:"windowSeconds"`
}

// TaskStartRecord represents a record of a Task start for quota tracking.
// Stored in AgentStatus to persist across controller restarts.
type TaskStartRecord struct {
	// TaskName is the name of the Task that was started.
	TaskName string `json:"taskName"`

	// TaskNamespace is the namespace of the Task.
	TaskNamespace string `json:"taskNamespace"`

	// StartTime is when the Task transitioned to Running phase.
	StartTime metav1.Time `json:"startTime"`
}

// ServerConfig enables Server mode for an Agent.
// When ServerConfig is present, the Agent runs as a persistent OpenCode server
// (Deployment + Service) instead of creating ephemeral Pods per Task.
// Tasks using a Server-mode Agent create lightweight Pods that connect to the
// server using `opencode run --attach`.
//
// Use Server mode for:
//   - Long-running agents (e.g., Slack bots, interactive assistants)
//   - Shared context across multiple Tasks (pre-loaded repositories)
//   - Avoiding cold start latency for each Task
type ServerConfig struct {
	// Port is the port OpenCode server listens on.
	// Defaults to 4096 if not specified.
	// +optional
	// +kubebuilder:default=4096
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`
}

// ServerStatus represents the observed state of a Server-mode Agent.
// This is only populated when ServerConfig is present in the Agent spec.
type ServerStatus struct {
	// DeploymentName is the name of the Kubernetes Deployment running the server.
	// Format: "{agent-name}-server"
	// +optional
	DeploymentName string `json:"deploymentName,omitempty"`

	// ServiceName is the name of the Kubernetes Service exposing the server.
	// Format: "{agent-name}"
	// +optional
	ServiceName string `json:"serviceName,omitempty"`

	// URL is the in-cluster URL to reach the OpenCode server.
	// Format: "http://{service-name}.{namespace}.svc.cluster.local:{port}"
	// Tasks use this URL with `opencode run --attach` to connect to the server.
	// +optional
	URL string `json:"url,omitempty"`

	// ReadyReplicas is the number of ready server pods from the Deployment.
	// For Server-mode, this is typically 0 or 1 (single replica only).
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`
}

// AgentSpec defines agent configuration
type AgentSpec struct {
	// AgentImage specifies the OpenCode init container image.
	// This image contains the OpenCode binary that gets copied to /tools volume.
	// The init container runs this image and copies the opencode binary to /tools/opencode.
	// If not specified, defaults to "quay.io/kubeopencode/kubeopencode-agent-opencode:latest".
	// +optional
	AgentImage string `json:"agentImage,omitempty"`

	// ExecutorImage specifies the main worker container image for task execution.
	// This is the development environment where tasks actually run.
	// The container uses /tools/opencode (provided by agentImage init container) to execute AI tasks.
	// If not specified, defaults to "quay.io/kubeopencode/kubeopencode-agent-devbox:latest".
	// +optional
	ExecutorImage string `json:"executorImage,omitempty"`

	// AttachImage specifies the lightweight image used for Server-mode --attach Pods.
	// When ServerConfig is set, Tasks create Pods that run `opencode run --attach <server-url>`.
	// These Pods only need the OpenCode binary and network access, not the full development
	// environment. Using a minimal image (~25MB) instead of devbox (~1GB) significantly
	// reduces image pull time and resource usage.
	//
	// If not specified, defaults to "quay.io/kubeopencode/kubeopencode-agent-attach:latest".
	// This field is ignored when ServerConfig is nil (Pod mode).
	// +optional
	AttachImage string `json:"attachImage,omitempty"`

	// WorkspaceDir specifies the working directory inside the agent container.
	// This is where task.md and context files are mounted.
	// The agent image must support the WORKSPACE_DIR environment variable.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^/.*`
	// +kubebuilder:validation:MinLength=1
	WorkspaceDir string `json:"workspaceDir"`

	// Command specifies the entrypoint command for the agent container.
	// This is optional and overrides the default ENTRYPOINT of the container image.
	//
	// If not specified, defaults to:
	//   ["sh", "-c", "/tools/opencode run \"$(cat ${WORKSPACE_DIR}/task.md)\""]
	//
	// The command defines HOW the agent executes tasks. Most users should not
	// need to customize this. Override only if you need custom execution behavior.
	//
	// ## Example
	//
	//   command: ["sh", "-c", "/tools/opencode run --format json \"$(cat /workspace/task.md)\""]
	//
	// +optional
	Command []string `json:"command,omitempty"`

	// Contexts provides default contexts for all tasks using this Agent.
	// These have the lowest priority in context merging.
	//
	// Context priority (lowest to highest):
	//   1. Agent.contexts (Agent-level defaults)
	//   2. Task.contexts (Task-specific contexts)
	//   3. Task.description (highest, becomes ${WORKSPACE_DIR}/task.md)
	//
	// Use this for organization-wide defaults like coding standards, security policies,
	// or common tool configurations that should apply to all tasks.
	// +optional
	Contexts []ContextItem `json:"contexts,omitempty"`

	// Config provides OpenCode configuration as a JSON string.
	// This configuration is written to /tools/opencode.json and the OPENCODE_CONFIG
	// environment variable is set to point to this file.
	//
	// The config should be a valid JSON object compatible with OpenCode's config schema.
	// See: https://opencode.ai/config.json for the schema.
	//
	// Example:
	//   config: |
	//     {
	//       "$schema": "https://opencode.ai/config.json",
	//       "model": "google/gemini-2.5-pro",
	//       "small_model": "google/gemini-2.5-flash"
	//     }
	// +optional
	Config *string `json:"config,omitempty"`

	// Credentials defines secrets that should be available to the agent.
	// Similar to GitHub Actions secrets, these can be mounted as files or
	// exposed as environment variables.
	//
	// Example use cases:
	//   - GitHub token for repository access (env: GITHUB_TOKEN)
	//   - SSH keys for git operations (file: ~/.ssh/id_rsa)
	//   - API keys for external services (env: ANTHROPIC_API_KEY)
	//   - Cloud credentials (file: ~/.config/gcloud/credentials.json)
	// +optional
	Credentials []Credential `json:"credentials,omitempty"`

	// PodSpec defines advanced Pod configuration for agent pods.
	// This includes labels, scheduling, runtime class, and other Pod-level settings.
	// Use this for fine-grained control over how agent pods are created.
	// +optional
	PodSpec *AgentPodSpec `json:"podSpec,omitempty"`

	// ServiceAccountName specifies the Kubernetes ServiceAccount to use for agent pods.
	// This controls what cluster resources the agent can access via RBAC.
	//
	// The ServiceAccount must exist in the Agent's namespace (where Pods run).
	// Users are responsible for creating the ServiceAccount and appropriate RBAC bindings
	// based on what permissions their agent needs.
	//
	// +required
	ServiceAccountName string `json:"serviceAccountName"`

	// AllowedNamespaces restricts which namespaces can reference this Agent.
	// This enables platform teams to control access to shared Agents.
	//
	// Supports glob patterns (e.g., "team-*", "prod-*", "dev-frontend").
	// Empty list means all namespaces are allowed (default: open to all).
	//
	// When a Task in namespace "foo" references this Agent and "foo" doesn't
	// match any pattern, the controller rejects the Task with an error condition.
	//
	// Example:
	//   allowedNamespaces:
	//     - "prod-*"      # All production namespaces
	//     - "staging"     # Staging namespace
	//     - "team-alpha"  # Specific team namespace
	// +optional
	AllowedNamespaces []string `json:"allowedNamespaces,omitempty"`

	// MaxConcurrentTasks limits the number of Tasks that can run concurrently
	// using this Agent. When the limit is reached, new Tasks will enter Queued
	// phase until capacity becomes available.
	//
	// This is useful when the Agent uses backend AI services with rate limits
	// (e.g., Claude, Gemini API quotas) to prevent overwhelming the service.
	//
	// - nil or 0: unlimited (default behavior, no concurrency limit)
	// - positive number: maximum number of Tasks that can be in Running phase
	//
	// Example:
	//   maxConcurrentTasks: 3  # Only 3 Tasks can run at once
	// +optional
	MaxConcurrentTasks *int32 `json:"maxConcurrentTasks,omitempty"`

	// Quota defines rate limiting for Task starts within a sliding time window.
	// When configured, Tasks will be queued if the quota is exceeded.
	// This is complementary to maxConcurrentTasks:
	//   - maxConcurrentTasks: limits how many Tasks run at once
	//   - quota: limits how quickly new Tasks can start
	//
	// Example:
	//   quota:
	//     maxTaskStarts: 10
	//     windowSeconds: 3600  # 10 tasks per hour
	// +optional
	Quota *QuotaConfig `json:"quota,omitempty"`

	// ServerConfig enables Server mode for this Agent.
	// When set, the Agent runs as a persistent OpenCode server (Deployment + Service)
	// instead of creating ephemeral Pods per Task.
	//
	// Server mode is useful for:
	//   - Long-running agents (e.g., Slack bots, interactive assistants)
	//   - Shared context across Tasks (pre-loaded repositories, faster startup)
	//   - Avoiding cold start latency for each Task
	//
	// When ServerConfig is nil (default), the Agent operates in Pod mode:
	// each Task creates a new Pod that runs to completion.
	//
	// In Server mode, Tasks create lightweight Pods that use `opencode run --attach`
	// to connect to the persistent server.
	//
	// Example:
	//   serverConfig:
	//     port: 4096
	// +optional
	ServerConfig *ServerConfig `json:"serverConfig,omitempty"`
}

// AgentStatus defines the observed state of Agent
type AgentStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the Agent's state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// TaskStartHistory tracks recent Task starts for quota enforcement.
	// The controller prunes entries older than the quota window automatically.
	// This is only populated when quota is configured on the Agent.
	// +optional
	// +listType=atomic
	TaskStartHistory []TaskStartRecord `json:"taskStartHistory,omitempty"`

	// ServerStatus contains the status of the OpenCode server when running in Server mode.
	// This is only populated when spec.serverConfig is set.
	// +optional
	ServerStatus *ServerStatus `json:"serverStatus,omitempty"`
}

// AgentPodSpec defines advanced Pod configuration for agent pods.
// This groups all Pod-level settings that control how the agent container runs.
// These settings apply to both Pod mode and Server mode.
type AgentPodSpec struct {
	// Labels defines additional labels to add to the agent pod.
	// These labels are applied to the Job's pod template and enable integration with:
	//   - NetworkPolicy podSelector for network isolation
	//   - Service selector for service discovery
	//   - PodMonitor/ServiceMonitor for Prometheus monitoring
	//   - Any other label-based pod selection
	//
	// Example: To make pods match a NetworkPolicy with podSelector:
	//   labels:
	//     network-policy: agent-restricted
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Scheduling defines pod scheduling configuration for agent pods.
	// This includes node selection, tolerations, and affinity rules.
	// +optional
	Scheduling *PodScheduling `json:"scheduling,omitempty"`

	// RuntimeClassName specifies the RuntimeClass to use for agent pods.
	// RuntimeClass provides a way to select container runtime configurations
	// such as gVisor (runsc) or Kata Containers for enhanced isolation.
	//
	// This is useful when running untrusted AI agent code that may generate
	// and execute arbitrary commands. Using gVisor or Kata provides an
	// additional layer of security beyond standard container isolation.
	//
	// The RuntimeClass must exist in the cluster before use.
	// Common values: "gvisor", "kata", "runc" (default if not specified)
	//
	// Example:
	//   runtimeClassName: gvisor
	//
	// See: https://kubernetes.io/docs/concepts/containers/runtime-class/
	// +optional
	RuntimeClassName *string `json:"runtimeClassName,omitempty"`

	// Resources specifies the compute resources (CPU, memory) for the agent container.
	// This applies to both Pod mode (per-Task Pods) and Server mode (Deployment).
	// If not specified, uses the cluster's default resource limits.
	//
	// Example:
	//   resources:
	//     requests:
	//       memory: "512Mi"
	//       cpu: "500m"
	//     limits:
	//       memory: "2Gi"
	//       cpu: "2"
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// PodScheduling defines scheduling configuration for agent pods.
// All fields are applied directly to the Job's pod template.
type PodScheduling struct {
	// NodeSelector specifies a selector for scheduling pods to specific nodes.
	// The pod will only be scheduled to nodes that have all the specified labels.
	//
	// Example:
	//   nodeSelector:
	//     kubernetes.io/os: linux
	//     node-type: gpu
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations allows pods to be scheduled on nodes with matching taints.
	//
	// Example:
	//   tolerations:
	//     - key: "dedicated"
	//       operator: "Equal"
	//       value: "ai-workload"
	//       effect: "NoSchedule"
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Affinity specifies affinity and anti-affinity rules for pods.
	// This enables advanced scheduling based on node attributes, pod co-location,
	// or pod anti-affinity for high availability.
	//
	// Example:
	//   affinity:
	//     nodeAffinity:
	//       requiredDuringSchedulingIgnoredDuringExecution:
	//         nodeSelectorTerms:
	//           - matchExpressions:
	//               - key: topology.kubernetes.io/zone
	//                 operator: In
	//                 values: ["us-west-2a", "us-west-2b"]
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
}

// Credential represents a secret that should be available to the agent.
// Each credential references a Kubernetes Secret and specifies how to expose it.
//
// Mounting behavior depends on whether SecretRef.Key is specified:
//
// 1. No Key specified + No MountPath: entire Secret as environment variables
// 2. No Key specified + MountPath: entire Secret as directory (each key becomes a file)
// 3. Key specified + Env: single key as environment variable
// 4. Key specified + MountPath: single key as file
type Credential struct {
	// Name is a descriptive name for this credential (for documentation purposes).
	// +required
	Name string `json:"name"`

	// SecretRef references the Kubernetes Secret containing the credential.
	// +required
	SecretRef SecretReference `json:"secretRef"`

	// MountPath specifies where to mount the secret.
	// - If SecretRef.Key is specified: mounts the single key's value as a file at this path.
	//   Example: "/home/agent/.ssh/id_rsa" for SSH keys
	// - If SecretRef.Key is not specified: mounts the entire Secret as a directory,
	//   where each key in the Secret becomes a file in the directory.
	//   Example: "/etc/ssl/certs" for a Secret containing ca.crt, client.crt, client.key
	// +optional
	MountPath *string `json:"mountPath,omitempty"`

	// Env specifies the environment variable name to expose the secret value.
	// Only applicable when SecretRef.Key is specified.
	// If specified, the secret key's value is set as this environment variable.
	// Example: "GITHUB_TOKEN" for GitHub API access
	// +optional
	Env *string `json:"env,omitempty"`

	// FileMode specifies the permission mode for mounted files.
	// Only applicable when MountPath is specified.
	// Defaults to 0600 (read/write for owner only) for security.
	// Use 0400 for read-only files like SSH keys.
	// +optional
	FileMode *int32 `json:"fileMode,omitempty"`
}

// SecretReference references a Kubernetes Secret.
// When Key is specified, only that specific key is used.
// When Key is omitted, the entire Secret is used (behavior depends on Credential.MountPath).
type SecretReference struct {
	// Name of the Secret.
	// +required
	Name string `json:"name"`

	// Key of the Secret to select.
	// If not specified, the entire Secret is used:
	// - With MountPath: mounted as a directory (each key becomes a file)
	// - Without MountPath: all keys become environment variables
	// When Key is omitted, the Env field on the Credential is ignored.
	// +optional
	Key *string `json:"key,omitempty"`
}

// ConfigMapKeySelector selects a key of a ConfigMap.
type ConfigMapKeySelector struct {
	// Name of the ConfigMap
	// +required
	Name string `json:"name"`

	// Key of the ConfigMap to select from
	// +required
	Key string `json:"key"`

	// Specify whether the ConfigMap must be defined
	// +optional
	Optional *bool `json:"optional,omitempty"`
}

// ConfigMapReference references an entire ConfigMap.
// Used with DirPath to mount all keys as files in a directory.
type ConfigMapReference struct {
	// Name of the ConfigMap
	// +required
	Name string `json:"name"`

	// Specify whether the ConfigMap must be defined
	// +optional
	Optional *bool `json:"optional,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AgentList contains a list of Agent
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Agent `json:"items"`
}
