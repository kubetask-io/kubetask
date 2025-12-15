// Copyright Contributors to the KubeTask project

// Package v1alpha1 contains the v1alpha1 API definitions
package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ContextType defines the type of context source
// +kubebuilder:validation:Enum=Inline;ConfigMap;Git
type ContextType string

const (
	// ContextTypeInline represents inline content
	ContextTypeInline ContextType = "Inline"

	// ContextTypeConfigMap represents content from a ConfigMap
	ContextTypeConfigMap ContextType = "ConfigMap"

	// ContextTypeGit represents content from a Git repository
	ContextTypeGit ContextType = "Git"
)

// InlineContext provides content directly in the YAML.
type InlineContext struct {
	// Content is the inline content to mount as a file.
	// +required
	Content string `json:"content"`
}

// ConfigMapContext references a ConfigMap for context content.
type ConfigMapContext struct {
	// Name of the ConfigMap
	// +required
	Name string `json:"name"`

	// Key specifies a single key to mount as a file.
	// If not specified, all keys are mounted as files in the directory.
	// +optional
	Key string `json:"key,omitempty"`

	// Optional specifies whether the ConfigMap must exist.
	// +optional
	Optional *bool `json:"optional,omitempty"`
}

// GitContext references content from a Git repository.
type GitContext struct {
	// Repository is the Git repository URL.
	// Example: "https://github.com/org/contexts"
	// +required
	Repository string `json:"repository"`

	// Path is the path within the repository to mount.
	// Can be a file or directory. If empty, the entire repository is mounted.
	//
	// Note on .git directory:
	//   - If Path is empty (entire repo): The mounted directory WILL contain .git/
	//   - If Path is specified (subdirectory): The mounted directory will NOT contain .git/
	//
	// Example: ".claude/", "docs/guide.md"
	// +optional
	Path string `json:"path,omitempty"`

	// Ref is the Git reference (branch, tag, or commit SHA).
	// Defaults to "HEAD" if not specified.
	// +optional
	// +kubebuilder:default="HEAD"
	Ref string `json:"ref,omitempty"`

	// Depth specifies the clone depth for shallow cloning.
	// 1 means shallow clone (fastest), 0 means full clone.
	// Defaults to 1 for efficiency.
	// +optional
	// +kubebuilder:default=1
	Depth *int `json:"depth,omitempty"`

	// SecretRef references a Secret containing Git credentials.
	// The Secret should contain one of:
	//   - "username" + "password": For HTTPS token-based auth (password can be a PAT)
	//   - "ssh-privatekey": For SSH key-based auth
	// If not specified, anonymous clone is attempted.
	// +optional
	SecretRef *GitSecretReference `json:"secretRef,omitempty"`
}

// GitSecretReference references a Secret for Git authentication.
type GitSecretReference struct {
	// Name of the Secret containing Git credentials.
	// +required
	Name string `json:"name"`
}

// ContextMount references a Context resource and specifies how to mount it.
// This allows the same Context to be mounted at different paths by different Tasks.
type ContextMount struct {
	// Name of the Context resource
	// +required
	Name string `json:"name"`

	// Namespace of the Context (optional, defaults to the referencing resource's namespace)
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// MountPath specifies where this context should be mounted in the agent pod.
	// If specified, the context content is written to this file path.
	// Example: "${WORKSPACE_DIR}/guides/coding-standards.md"
	//
	// If NOT specified (empty), the context content is appended to ${WORKSPACE_DIR}/task.md
	// (where WORKSPACE_DIR is configured in Agent.spec.workspaceDir, defaulting to "/workspace")
	// in a structured XML format:
	//   <context name="coding-standards" namespace="default" type="File">
	//   ... content ...
	//   </context>
	//
	// This allows multiple contexts to be aggregated into a single task.md file,
	// which the agent can parse and understand.
	// +optional
	MountPath string `json:"mountPath,omitempty"`
}

// TaskPhase represents the current phase of a task
// +kubebuilder:validation:Enum=Pending;Queued;Running;Completed;Failed
type TaskPhase string

const (
	// TaskPhasePending means the task has not started yet
	TaskPhasePending TaskPhase = "Pending"
	// TaskPhaseQueued means the task is waiting for Agent capacity.
	// This occurs when the Agent has maxConcurrentTasks set and the limit is reached.
	// The task will automatically transition to Running when capacity becomes available.
	TaskPhaseQueued TaskPhase = "Queued"
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
// +kubebuilder:resource:scope="Namespaced",shortName=tk
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
	// Description is the task instruction/prompt.
	// The controller creates ${WORKSPACE_DIR}/task.md with this content
	// (where WORKSPACE_DIR is configured in Agent.spec.workspaceDir, defaulting to "/workspace").
	// This is the primary way to tell the agent what to do.
	//
	// Example:
	//   description: "Update all dependencies and create a PR"
	// +optional
	Description *string `json:"description,omitempty"`

	// Contexts references Context CRDs to include in this task.
	// Each ContextMount specifies which Context to use and where to mount it.
	//
	// Context priority (lowest to highest):
	//   1. Agent.contexts (Agent-level defaults)
	//   2. Task.contexts (Task-specific contexts)
	//   3. Task.description (highest, becomes ${WORKSPACE_DIR}/task.md)
	// +optional
	Contexts []ContextMount `json:"contexts,omitempty"`

	// AgentRef references an Agent for this task.
	// If not specified, uses the "default" Agent in the same namespace.
	// +optional
	AgentRef string `json:"agentRef,omitempty"`

	// HumanInTheLoop configures whether this task requires human participation.
	// When enabled, the agent container will remain running after task completion,
	// allowing users to exec into the container for debugging, review, or manual intervention.
	//
	// IMPORTANT: When humanInTheLoop is enabled, the Agent MUST also specify the Command field.
	// The controller wraps the command to add a sleep after completion.
	// Without Command in the Agent, the controller cannot wrap the entrypoint.
	// +optional
	HumanInTheLoop *HumanInTheLoop `json:"humanInTheLoop,omitempty"`
}

// TaskExecutionStatus defines the observed state of Task
type TaskExecutionStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

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
	// +listType=map
	// +listMapKey=type
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

// AgentSpec defines agent configuration
type AgentSpec struct {
	// Agent container image to use for task execution.
	// The controller generates Jobs with this image.
	// If not specified, defaults to "quay.io/kubetask/kubetask-agent:latest".
	// +optional
	AgentImage string `json:"agentImage,omitempty"`

	// WorkspaceDir specifies the working directory inside the agent container.
	// This is where task.md and context files are mounted.
	// The agent image must support the WORKSPACE_DIR environment variable.
	// Defaults to "/workspace" if not specified.
	// +optional
	// +kubebuilder:default="/workspace"
	// +kubebuilder:validation:Pattern=`^/.*`
	WorkspaceDir string `json:"workspaceDir,omitempty"`

	// Command specifies the entrypoint command for the agent container.
	// This is REQUIRED and overrides the default ENTRYPOINT of the container image.
	//
	// The command defines HOW the agent executes tasks. Different users can
	// customize execution behavior (e.g., output format, flags) without
	// modifying the agent image. The agent image only provides the tools.
	//
	// When Task.spec.humanInTheLoop is enabled, the controller wraps this
	// command with a sleep to keep the container running after task completion.
	//
	// Example:
	//   command: ["sh", "-c", "gemini --yolo -p \"$(cat /workspace/task.md)\""]
	//
	// When humanInTheLoop is enabled on a Task, the command will be wrapped to:
	//   sh -c 'original-command; sleep $KUBETASK_KEEP_ALIVE_SECONDS'
	// +required
	// +kubebuilder:validation:MinItems=1
	Command []string `json:"command"`

	// Contexts references Context CRDs as defaults for all tasks using this Agent.
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
	Contexts []ContextMount `json:"contexts,omitempty"`

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
	// The ServiceAccount must exist in the same namespace where tasks are created.
	// Users are responsible for creating the ServiceAccount and appropriate RBAC bindings
	// based on what permissions their agent needs.
	//
	// +required
	ServiceAccountName string `json:"serviceAccountName"`

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
}

// AgentPodSpec defines advanced Pod configuration for agent pods.
// This groups all Pod-level settings that control how the agent container runs.
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

// HumanInTheLoop configures human participation requirements for an agent.
// When enabled, the agent container remains running after task completion,
// allowing users to kubectl exec into the container for debugging or review.
type HumanInTheLoop struct {
	// Enabled indicates whether human-in-the-loop mode is active.
	// When true, the agent container will sleep after task completion
	// instead of exiting immediately.
	// +required
	Enabled bool `json:"enabled"`

	// KeepAlive specifies how long the container should remain running
	// after task completion, allowing time for human interaction.
	// Users can kubectl exec into the container during this period.
	// Uses standard Go duration format (e.g., "1h", "30m", "1h30m").
	// Defaults to "1h" (1 hour) if not specified when enabled is true.
	// +optional
	KeepAlive *metav1.Duration `json:"keepAlive,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope="Namespaced",shortName=ktc
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

// ConcurrencyPolicy describes how the CronTask will handle concurrent executions.
// +kubebuilder:validation:Enum=Allow;Forbid;Replace
type ConcurrencyPolicy string

const (
	// AllowConcurrent allows multiple Tasks to run concurrently.
	AllowConcurrent ConcurrencyPolicy = "Allow"

	// ForbidConcurrent forbids concurrent runs, skipping next run if previous
	// hasn't finished yet.
	ForbidConcurrent ConcurrencyPolicy = "Forbid"

	// ReplaceConcurrent cancels currently running Task and replaces it with a new one.
	ReplaceConcurrent ConcurrencyPolicy = "Replace"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced",shortName=ctk
// +kubebuilder:printcolumn:JSONPath=`.spec.schedule`,name="Schedule",type=string
// +kubebuilder:printcolumn:JSONPath=`.spec.suspend`,name="Suspend",type=boolean
// +kubebuilder:printcolumn:JSONPath=`.status.lastScheduleTime`,name="Last Schedule",type=date
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// CronTask represents a scheduled task that runs on a cron schedule.
// CronTask creates Task resources at scheduled times, similar to how
// Kubernetes CronJob creates Jobs.
type CronTask struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of CronTask
	Spec CronTaskSpec `json:"spec"`

	// Status represents the current status of the CronTask
	// +optional
	Status CronTaskStatus `json:"status,omitempty"`
}

// CronTaskSpec defines the CronTask configuration
type CronTaskSpec struct {
	// Schedule specifies the cron schedule in standard cron format.
	// Example: "0 9 * * *" runs at 9:00 AM every day.
	// +required
	Schedule string `json:"schedule"`

	// ConcurrencyPolicy specifies how to treat concurrent executions of a Task.
	// Valid values are:
	// - "Allow" (default): allows Tasks to run concurrently
	// - "Forbid": forbids concurrent runs, skipping next run if previous hasn't finished
	// - "Replace": cancels currently running Task and replaces it with a new one
	// +optional
	// +kubebuilder:default=Forbid
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`

	// Suspend tells the controller to suspend subsequent executions.
	// It does not apply to already started executions.
	// Defaults to false.
	// +optional
	Suspend *bool `json:"suspend,omitempty"`

	// SuccessfulTasksHistoryLimit specifies how many completed Tasks should be kept.
	// Defaults to 3.
	// +optional
	// +kubebuilder:default=3
	SuccessfulTasksHistoryLimit *int32 `json:"successfulTasksHistoryLimit,omitempty"`

	// FailedTasksHistoryLimit specifies how many failed Tasks should be kept.
	// Defaults to 1.
	// +optional
	// +kubebuilder:default=1
	FailedTasksHistoryLimit *int32 `json:"failedTasksHistoryLimit,omitempty"`

	// TaskTemplate is the template for the Task that will be created when the schedule triggers.
	// +required
	TaskTemplate TaskTemplateSpec `json:"taskTemplate"`
}

// TaskTemplateSpec defines the template for creating Tasks
type TaskTemplateSpec struct {
	// Metadata for the created Task.
	// Labels and annotations from this field are merged with those generated by the controller.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the TaskSpec that will be used to create Tasks.
	// +required
	Spec TaskSpec `json:"spec"`
}

// CronTaskStatus defines the observed state of CronTask
type CronTaskStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Active is a list of references to currently running Tasks.
	// +optional
	Active []corev1.ObjectReference `json:"active,omitempty"`

	// LastScheduleTime is the last time a Task was successfully scheduled.
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`

	// LastSuccessfulTime is the last time a Task completed successfully.
	// +optional
	LastSuccessfulTime *metav1.Time `json:"lastSuccessfulTime,omitempty"`

	// Conditions represent the latest available observations of the CronTask's state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CronTaskList contains a list of CronTask
type CronTaskList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CronTask `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced",shortName=ctx
// +kubebuilder:printcolumn:JSONPath=`.spec.type`,name="Type",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// Context represents a reusable context resource for AI agent tasks.
// Context is the top-level API for managing reusable context content that can be
// shared across multiple Tasks and Agents.
//
// Unlike inline contexts (ContextItem), Context CRs enable:
//   - Reusability: Share the same context across multiple Tasks
//   - Independent lifecycle: Update context without modifying Tasks
//   - Version control: Track context changes in Git
//   - Separation of concerns: Context content vs. mount location
//
// The mount path is NOT defined in Context - it's specified by the referencing
// Task or Agent via ContextMount.mountPath. This allows the same Context to be
// mounted at different paths by different consumers.
type Context struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the context configuration
	Spec ContextSpec `json:"spec"`

	// Status represents the current status of the Context
	// +optional
	Status ContextStatus `json:"status,omitempty"`
}

// ContextSpec defines the Context configuration.
// Context uses the same simplified structure as ContextItem but without mountPath,
// since the mount path is specified by the referencing Task/Agent via ContextMount.
type ContextSpec struct {
	// Type of context source: Inline, ConfigMap, or Git
	// +required
	Type ContextType `json:"type"`

	// Inline context (required when Type == "Inline")
	// +optional
	Inline *InlineContext `json:"inline,omitempty"`

	// ConfigMap context (required when Type == "ConfigMap")
	// +optional
	ConfigMap *ConfigMapContext `json:"configMap,omitempty"`

	// Git context (required when Type == "Git")
	// +optional
	Git *GitContext `json:"git,omitempty"`
}

// ContextStatus defines the observed state of Context
type ContextStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the Context's state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ContextList contains a list of Context
type ContextList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Context `json:"items"`
}
