// Copyright Contributors to the KubeTask project

// Package v1alpha1 contains the v1alpha1 API definitions
package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ContextType defines the type of context
// +kubebuilder:validation:Enum=File
type ContextType string

const (
	// ContextTypeFile represents a file context (task.md, guide.md, etc.)
	ContextTypeFile ContextType = "File"

	// Future context types:
	// ContextTypeMCP ContextType = "MCP"
)

// Context represents different types of task inputs
// This is a polymorphic type that can represent File, MCP, etc.
type Context struct {
	// Type of context: File, MCP, etc.
	// +required
	Type ContextType `json:"type"`

	// File context (required when Type == "File")
	// +optional
	File *FileContext `json:"file,omitempty"`

	// Future context types can be added here:
	// MCP *MCPContext `json:"mcp,omitempty"`
}

// FileContext represents a file or directory with content from various sources.
// Use FilePath for single files (with Inline or ConfigMapKeyRef source).
// Use DirPath for directories (with ConfigMapRef source - all keys become files).
type FileContext struct {
	// FilePath is the full path where this file will be mounted in the agent pod.
	// Use this for single file content (with Inline or ConfigMapKeyRef source).
	// Multiple contexts with the same FilePath will be aggregated into a single file.
	// Example: "/workspace/task.md", "/workspace/config/settings.json"
	// Either FilePath or DirPath must be specified, but not both.
	// +optional
	FilePath string `json:"filePath,omitempty"`

	// DirPath is the directory path where files will be mounted in the agent pod.
	// Use this with ConfigMapRef to mount all keys in a ConfigMap as files.
	// Example: "/workspace/docs" - each key in the ConfigMap becomes a file.
	// Either FilePath or DirPath must be specified, but not both.
	// +optional
	DirPath string `json:"dirPath,omitempty"`

	// File content source (exactly one must be specified)
	// +required
	Source FileSource `json:"source"`
}

// FileSource represents a source for file content
type FileSource struct {
	// Inline content (use with FilePath)
	// +optional
	Inline *string `json:"inline,omitempty"`

	// Reference to a key in a ConfigMap (use with FilePath)
	// +optional
	ConfigMapKeyRef *ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`

	// Reference to an entire ConfigMap (use with DirPath)
	// All keys in the ConfigMap will be mounted as files in the directory.
	// +optional
	ConfigMapRef *ConfigMapReference `json:"configMapRef,omitempty"`
}

// TaskPhase represents the current phase of a task
// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed
type TaskPhase string

const (
	// TaskPhasePending means the task has not started yet
	TaskPhasePending TaskPhase = "Pending"
	// TaskPhaseRunning means the task is currently executing
	TaskPhaseRunning TaskPhase = "Running"
	// TaskPhaseCompleted means the task execution finished (Job exited with code 0).
	// This indicates the agent completed its work, not necessarily that the task "succeeded".
	// The actual outcome should be determined by examining the agent's output.
	TaskPhaseCompleted TaskPhase = "Completed"
	// TaskPhaseFailed means the task had an infrastructure failure
	// (e.g., Job crashed, unable to schedule, missing Agent).
	TaskPhaseFailed TaskPhase = "Failed"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced"
// +kubebuilder:printcolumn:JSONPath=`.status.phase`,name="Phase",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.jobName`,name="Job",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// Task represents a single task execution.
// Task is the primary API for users who want to execute AI-powered tasks.
type Task struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of Task
	Spec TaskSpec `json:"spec"`

	// Status represents the current status of the Task
	// +optional
	Status TaskExecutionStatus `json:"status,omitempty"`
}

// TaskSpec defines the Task configuration
type TaskSpec struct {
	// Contexts defines what this task operates on
	// This includes files, repositories, and other context types
	// Example: [task.md file, guide.md file, repository context]
	// +required
	Contexts []Context `json:"contexts"`

	// AgentRef references an Agent for this task.
	// If not specified, uses the "default" Agent in the same namespace.
	// +optional
	AgentRef string `json:"agentRef,omitempty"`
}

// TaskExecutionStatus defines the observed state of Task
type TaskExecutionStatus struct {
	// Execution phase
	// +optional
	Phase TaskPhase `json:"phase,omitempty"`

	// Kubernetes Job name
	// +optional
	JobName string `json:"jobName,omitempty"`

	// Start time
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Completion time
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Kubernetes standard conditions
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// TaskList contains a list of Task
type TaskList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Task `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope="Namespaced"
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// Agent defines the AI agent configuration for task execution.
// Agent = AI agent + permissions + tools + infrastructure
// This is the execution black box - Task creators don't need to understand execution details.
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the agent configuration
	Spec AgentSpec `json:"spec"`
}

// AgentSpec defines agent configuration
type AgentSpec struct {
	// Agent container image to use for task execution.
	// The controller generates Jobs with this image.
	// If not specified, defaults to "quay.io/zhaoxue/kubetask-agent:latest".
	// +optional
	AgentImage string `json:"agentImage,omitempty"`

	// HumanInTheLoop configures whether tasks using this agent require human participation.
	// When enabled, the agent container will remain running after task completion,
	// allowing users to exec into the container for debugging, review, or manual intervention.
	//
	// IMPORTANT: When humanInTheLoop is enabled, you MUST also specify the Command field.
	// The controller wraps the command to add a sleep after completion.
	// Without Command, the controller cannot wrap the entrypoint.
	// +optional
	HumanInTheLoop *HumanInTheLoop `json:"humanInTheLoop,omitempty"`

	// Command specifies the entrypoint command for the agent container.
	// This overrides the default ENTRYPOINT of the container image.
	//
	// This field is REQUIRED when humanInTheLoop is enabled, as the controller
	// needs to wrap the command with a sleep to keep the container running.
	//
	// Example:
	//   command: ["sh", "-c", "gemini --yolo -p \"$(cat /workspace/task.md)\""]
	//
	// The command will be wrapped to:
	//   sh -c 'original-command; sleep $KUBETASK_KEEP_ALIVE_SECONDS'
	// +optional
	Command []string `json:"command,omitempty"`

	// DefaultContexts defines the base-level contexts that are included in all tasks
	// using this Agent. These contexts are applied at the lowest priority,
	// meaning task-specific contexts take precedence.
	//
	// Context priority (lowest to highest):
	//   1. Agent.defaultContexts (base layer)
	//   2. Task.contexts (task-specific contexts)
	//
	// Use this for organization-wide defaults like coding standards, security policies,
	// or common tool configurations that should apply to all tasks.
	// +optional
	DefaultContexts []Context `json:"defaultContexts,omitempty"`

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

	// PodLabels defines additional labels to add to the agent pod.
	// These labels are applied to the Job's pod template and enable integration with:
	//   - NetworkPolicy podSelector for network isolation
	//   - Service selector for service discovery
	//   - PodMonitor/ServiceMonitor for Prometheus monitoring
	//   - Any other label-based pod selection
	//
	// Example: To make pods match a NetworkPolicy with podSelector:
	//   podLabels:
	//     network-policy: agent-restricted
	// +optional
	PodLabels map[string]string `json:"podLabels,omitempty"`

	// Scheduling defines pod scheduling configuration for agent pods.
	// This includes node selection, tolerations, and affinity rules.
	// +optional
	Scheduling *PodScheduling `json:"scheduling,omitempty"`

	// ServiceAccountName specifies the Kubernetes ServiceAccount to use for agent pods.
	// This controls what cluster resources the agent can access via RBAC.
	//
	// The ServiceAccount must exist in the same namespace where tasks are created.
	// Users are responsible for creating the ServiceAccount and appropriate RBAC bindings
	// based on what permissions their agent needs.
	//
	// +required
	ServiceAccountName string `json:"serviceAccountName"`
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
type Credential struct {
	// Name is a descriptive name for this credential (for documentation purposes).
	// +required
	Name string `json:"name"`

	// SecretRef references the Kubernetes Secret containing the credential.
	// +required
	SecretRef SecretReference `json:"secretRef"`

	// MountPath specifies where to mount the secret as a file.
	// If specified, the secret key's value is written to this path.
	// Example: "/home/agent/.ssh/id_rsa" for SSH keys
	// +optional
	MountPath *string `json:"mountPath,omitempty"`

	// Env specifies the environment variable name to expose the secret value.
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

// SecretReference references a specific key in a Kubernetes Secret.
type SecretReference struct {
	// Name of the Secret.
	// +required
	Name string `json:"name"`

	// Key of the Secret to select.
	// +required
	Key string `json:"key"`
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

// HumanInTheLoop configures human participation requirements for an agent.
// When enabled, the agent container remains running after task completion,
// allowing users to kubectl exec into the container for debugging or review.
type HumanInTheLoop struct {
	// Enabled indicates whether human-in-the-loop mode is active.
	// When true, the agent container will sleep after task completion
	// instead of exiting immediately.
	// +required
	Enabled bool `json:"enabled"`

	// KeepAliveSeconds specifies how long the container should remain running
	// after task completion, allowing time for human interaction.
	// Users can kubectl exec into the container during this period.
	// Defaults to 3600 (1 hour) if not specified when enabled is true.
	// +optional
	// +kubebuilder:default=3600
	KeepAliveSeconds *int32 `json:"keepAliveSeconds,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope="Namespaced"
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// KubeTaskConfig defines system-level configuration for KubeTask.
// This CRD provides cluster or namespace-level settings for task lifecycle management,
// including TTL-based cleanup and future archive capabilities.
type KubeTaskConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the KubeTask configuration
	Spec KubeTaskConfigSpec `json:"spec"`
}

// KubeTaskConfigSpec defines the system-level configuration
type KubeTaskConfigSpec struct {
	// TaskLifecycle configures task lifecycle management including cleanup policies.
	// +optional
	TaskLifecycle *TaskLifecycleConfig `json:"taskLifecycle,omitempty"`
}

// TaskLifecycleConfig defines task lifecycle management settings
type TaskLifecycleConfig struct {
	// TTLSecondsAfterFinished specifies how long completed or failed Tasks
	// should be retained before automatic deletion.
	// The timer starts when a Task enters Completed or Failed phase.
	// Associated Jobs and ConfigMaps are deleted via OwnerReference cascade.
	// Defaults to 604800 (7 days) if not specified.
	// Set to 0 to disable automatic cleanup.
	// +optional
	// +kubebuilder:default=604800
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// KubeTaskConfigList contains a list of KubeTaskConfig
type KubeTaskConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeTaskConfig `json:"items"`
}
