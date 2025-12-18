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
//
// Deprecated: Use ContextSource with ContextRef instead for new code.
// This type is kept for backward compatibility but is no longer used in TaskSpec/AgentSpec.
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

// ContextSource represents a context that can be either a reference to a Context CRD
// or an inline definition. Exactly one of Ref or Inline must be specified.
//
// This follows the same pattern as WorkflowRunSpec (workflowRef vs inline),
// allowing users to choose between reusable Context CRDs or one-off inline definitions.
type ContextSource struct {
	// Ref references an existing Context CRD by name.
	// Use this for reusable contexts that are managed independently.
	// +optional
	Ref *ContextRef `json:"ref,omitempty"`

	// Inline defines context content directly in the Task/Agent.
	// Use this for one-off contexts that don't need to be reused.
	// +optional
	Inline *ContextItem `json:"inline,omitempty"`
}

// ContextRef references a Context CRD and specifies how to mount it.
// This is similar to ContextMount but used within ContextSource.
type ContextRef struct {
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
	// in a structured XML format.
	// +optional
	MountPath string `json:"mountPath,omitempty"`
}

// ContextItem defines inline context with content and mount path.
// This is the inline version of Context CRD, used directly in Task/Agent specs.
type ContextItem struct {
	// Type of context source: Inline, ConfigMap, or Git
	// +required
	Type ContextType `json:"type"`

	// MountPath specifies where this context should be mounted in the agent pod.
	// Same semantics as ContextRef.MountPath.
	// +optional
	MountPath string `json:"mountPath,omitempty"`

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

	// Contexts provides additional context for the task.
	// Each context can be a reference to a Context CRD (via Ref) or an inline definition (via Inline).
	// Contexts are processed in array order, with later contexts taking precedence.
	//
	// Context priority (lowest to highest):
	//   1. Agent.contexts (Agent-level defaults)
	//   2. Task.contexts (Task-specific contexts)
	//   3. Task.description (highest, becomes ${WORKSPACE_DIR}/task.md)
	//
	// Example:
	//   contexts:
	//     - ref:
	//         name: coding-standards
	//     - inline:
	//         type: Git
	//         mountPath: ${WORKSPACE_DIR}
	//         git:
	//           repository: https://github.com/org/repo
	//           ref: main
	// +optional
	Contexts []ContextSource `json:"contexts,omitempty"`

	// AgentRef references an Agent for this task.
	// If not specified, uses the "default" Agent in the same namespace.
	// +optional
	AgentRef string `json:"agentRef,omitempty"`
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
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^/.*`
	// +kubebuilder:validation:MinLength=1
	WorkspaceDir string `json:"workspaceDir"`

	// Command specifies the entrypoint command for the agent container.
	// This is REQUIRED and overrides the default ENTRYPOINT of the container image.
	//
	// The command defines HOW the agent executes tasks. Different users can
	// customize execution behavior (e.g., output format, flags) without
	// modifying the agent image. The agent image only provides the tools.
	//
	// ## humanInTheLoop Integration
	//
	// When Task.spec.humanInTheLoop is enabled, the controller wraps this command
	// to keep the container running after task completion, allowing interactive
	// debugging via `kubectl exec`.
	//
	// The wrapping behavior depends on the command format:
	//
	// 1. For "sh -c <script>" format (recommended):
	//    The script is wrapped in a subshell to isolate exit/exec:
	//      sh -c '( <script> ); EXIT_CODE=$?; sleep N; exit $EXIT_CODE'
	//
	// 2. For other formats (e.g., ["python", "-c", "..."]):
	//    Arguments are passed via $@ to preserve special characters:
	//      sh -c '"$@"; EXIT_CODE=$?; sleep N; exit $EXIT_CODE' -- python -c ...
	//
	// ## Best Practices
	//
	// - Use ["sh", "-c", "<script>"] format for shell scripts
	// - Avoid using 'exec' as it replaces the shell process (though subshell
	//   isolation mitigates this for humanInTheLoop)
	// - Commands with 'exit' work correctly - the exit code is captured and
	//   the sleep still executes
	//
	// ## Example
	//
	//   command: ["sh", "-c", "gemini --yolo -p \"$(cat /workspace/task.md)\""]
	//
	// +required
	// +kubebuilder:validation:MinItems=1
	Command []string `json:"command"`

	// Contexts provides default contexts for all tasks using this Agent.
	// Each context can be a reference to a Context CRD (via Ref) or an inline definition (via Inline).
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
	Contexts []ContextSource `json:"contexts,omitempty"`

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

	// HumanInTheLoop configures default human-in-the-loop settings for all tasks
	// using this Agent. Individual Tasks can override these settings.
	//
	// When enabled at the Agent level, all Tasks using this Agent will have
	// humanInTheLoop behavior unless the Task explicitly disables it.
	//
	// Override behavior:
	//   - Task.spec.humanInTheLoop takes precedence over Agent.spec.humanInTheLoop
	//   - If Task.spec.humanInTheLoop is nil, Agent settings are used
	//   - If Task.spec.humanInTheLoop is set (even with enabled=false), it overrides Agent
	//
	// Example:
	//   # Agent with default humanInTheLoop settings
	//   spec:
	//     humanInTheLoop:
	//       enabled: true
	//       duration: "1h"
	//       ports:
	//         - name: dev-server
	//           containerPort: 3000
	// +optional
	HumanInTheLoop *HumanInTheLoop `json:"humanInTheLoop,omitempty"`
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
// When enabled, a session sidecar container is added to the Pod, allowing
// users to kubectl exec into it for debugging, review, or manual intervention
// after the main agent container completes.
//
// The sidecar container shares the same workspace volume and environment
// variables as the agent container, so users can use the same tools
// (e.g., gemini, claude CLI) to interact with the workspace.
//
// Note: This configuration is only available on Agent, not on Task.
// Tasks inherit the humanInTheLoop settings from their referenced Agent.
type HumanInTheLoop struct {
	// Enabled indicates whether human-in-the-loop mode is active.
	// When true, a session sidecar container is added to the Pod.
	// +required
	Enabled bool `json:"enabled"`

	// Duration specifies how long the session sidecar container should remain running,
	// allowing time for human interaction.
	// Users can kubectl exec into the sidecar during this period.
	// Uses standard Go duration format (e.g., "1h", "30m", "1h30m").
	// Defaults to "1h" (1 hour) if not specified when enabled is true.
	//
	// Mutually exclusive with Command - only one can be specified.
	// If neither is specified, defaults to "1h".
	// +optional
	Duration *metav1.Duration `json:"duration,omitempty"`

	// Command specifies a custom command to run in the session sidecar container.
	// Use this for advanced scenarios like running code-server or other services.
	//
	// Mutually exclusive with Duration - only one can be specified.
	// If both are specified, it is a configuration error.
	//
	// Example:
	//   command: ["sh", "-c", "code-server --bind-addr 0.0.0.0:8080 ${WORKSPACE_DIR} & sleep 7200"]
	//
	// +optional
	Command []string `json:"command,omitempty"`

	// Image specifies the container image for the session sidecar.
	// If not specified, defaults to the Agent's agentImage, which allows
	// users to use the same tools (e.g., gemini, claude) in the sidecar.
	//
	// You can specify a lightweight image (e.g., "busybox:stable") to
	// reduce resource usage, or a custom debug image with additional tools.
	// +optional
	Image string `json:"image,omitempty"`

	// Ports specifies container ports to expose on the sidecar for port-forwarding.
	// These ports can be accessed via `kubectl port-forward` during
	// the human-in-the-loop session, enabling developers to test
	// development servers, APIs, or other network services.
	//
	// Example:
	//   ports:
	//     - name: dev-server
	//       containerPort: 3000
	//     - name: code-server
	//       containerPort: 8080
	//
	// Access via:
	//   kubectl port-forward <pod-name> 3000:3000 8080:8080
	// +optional
	Ports []ContainerPort `json:"ports,omitempty"`
}

// ContainerPort defines a port to expose on the agent container.
// This is a simplified version of corev1.ContainerPort that only exposes
// fields relevant for port-forwarding use cases.
type ContainerPort struct {
	// Name is an optional name for this port.
	// Used for documentation and service discovery purposes.
	// Must be unique within the ports list.
	// +optional
	Name string `json:"name,omitempty"`

	// ContainerPort is the port number to expose on the container.
	// This is the port that the application inside the container listens on.
	// +required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	ContainerPort int32 `json:"containerPort"`

	// Protocol is the protocol for this port.
	// Must be UDP or TCP.
	// Defaults to "TCP".
	// +optional
	// +kubebuilder:default="TCP"
	Protocol corev1.Protocol `json:"protocol,omitempty"`
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

// WorkflowPhase represents the current phase of a workflow
// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed
type WorkflowPhase string

const (
	// WorkflowPhasePending means the workflow has been initialized but not started
	WorkflowPhasePending WorkflowPhase = "Pending"
	// WorkflowPhaseRunning means the workflow is currently executing stages
	WorkflowPhaseRunning WorkflowPhase = "Running"
	// WorkflowPhaseCompleted means all stages completed successfully
	WorkflowPhaseCompleted WorkflowPhase = "Completed"
	// WorkflowPhaseFailed means at least one task in the workflow failed
	WorkflowPhaseFailed WorkflowPhase = "Failed"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope="Namespaced",shortName=wf
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// Workflow represents a reusable workflow template for multi-stage task orchestration.
// Workflow is a pure template definition - it does not execute by itself.
// To execute a workflow, create a WorkflowRun that references this Workflow.
//
// Workflow defines a sequence of stages, where each stage contains one or more
// Tasks that run in parallel. Stages execute sequentially - stage N+1 starts
// only after all tasks in stage N complete successfully.
//
// Example:
//
//	workflow = [[task] -> [task, task, task] -> [task]]
//
// This creates 3 stages where:
//   - Stage 0: runs 1 task
//   - Stage 1: runs 3 tasks in parallel (starts after stage 0 completes)
//   - Stage 2: runs 1 task (starts after all stage 1 tasks complete)
//
// Usage:
//
//	apiVersion: kubetask.io/v1alpha1
//	kind: Workflow
//	metadata:
//	  name: ci-pipeline
//	spec:
//	  stages:
//	    - tasks:
//	        - name: lint
//	          spec:
//	            description: "Run linting"
//	---
//	apiVersion: kubetask.io/v1alpha1
//	kind: WorkflowRun
//	metadata:
//	  name: ci-pipeline-run-001
//	spec:
//	  workflowRef: ci-pipeline
type Workflow struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the workflow template configuration
	Spec WorkflowSpec `json:"spec"`
	// Note: Workflow has no Status - it's a template, not an execution.
	// Use WorkflowRun to execute a workflow and track status.
}

// WorkflowSpec defines the Workflow configuration
type WorkflowSpec struct {
	// Stages defines the sequential stages of the workflow.
	// Each stage contains one or more tasks that run in parallel.
	// Stages are executed sequentially - stage N+1 starts only after
	// all tasks in stage N complete successfully.
	// +required
	// +kubebuilder:validation:MinItems=1
	Stages []WorkflowStage `json:"stages"`
}

// WorkflowStage defines a stage in the workflow containing parallel tasks.
type WorkflowStage struct {
	// Name is an optional name for this stage.
	// If not specified, auto-generated as "stage-0", "stage-1", etc.
	// based on the stage's index in the stages array.
	// +optional
	Name string `json:"name,omitempty"`

	// Tasks defines the tasks to run in parallel within this stage.
	// All tasks in a stage start simultaneously and must all complete
	// successfully before the next stage begins.
	// +required
	// +kubebuilder:validation:MinItems=1
	Tasks []WorkflowTask `json:"tasks"`
}

// WorkflowTask defines a task within a workflow stage.
// Each task will be created as a Task CR when its stage starts.
type WorkflowTask struct {
	// Name is a required unique name for this task within the workflow.
	// The actual Task CR name will be "{workflow-name}-{task-name}".
	// Must be a valid Kubernetes name (lowercase, alphanumeric, hyphens).
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=53
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// Spec is the TaskSpec that will be used to create the Task.
	// Each task should specify its own agentRef as needed.
	// +required
	Spec TaskSpec `json:"spec"`
}

// WorkflowStageStatus contains status information for a single stage.
// Used by WorkflowRunStatus to track per-stage execution status.
type WorkflowStageStatus struct {
	// Name is the name of the stage (auto-generated if not specified in spec).
	Name string `json:"name"`

	// Phase is the current phase of this stage.
	// +optional
	Phase WorkflowPhase `json:"phase,omitempty"`

	// Tasks contains the names of Task CRs created for this stage.
	// These are the actual Task CR names (e.g., "my-workflow-lint").
	// +optional
	Tasks []string `json:"tasks,omitempty"`

	// StartTime is when this stage started executing.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when this stage finished.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// WorkflowList contains a list of Workflow
type WorkflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workflow `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced",shortName=wfr
// +kubebuilder:printcolumn:JSONPath=`.status.phase`,name="Phase",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.currentStage`,name="Stage",type=integer
// +kubebuilder:printcolumn:JSONPath=`.status.completedTasks`,name="Completed",type=integer
// +kubebuilder:printcolumn:JSONPath=`.status.totalTasks`,name="Total",type=integer
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// WorkflowRun represents an execution instance of a Workflow.
// WorkflowRun can be created in two ways:
//  1. Reference an existing Workflow template via workflowRef
//  2. Define an inline workflow via inline spec
//
// WorkflowRun is the owner of all Task CRs it creates.
// When a WorkflowRun is deleted, all its child Tasks are garbage collected.
//
// WorkflowRun follows the same TTL cleanup policy as Task, configured
// in KubeTaskConfig.spec.taskLifecycle.ttlSecondsAfterFinished.
type WorkflowRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines what workflow to execute
	Spec WorkflowRunSpec `json:"spec"`

	// Status represents the current execution status
	// +optional
	Status WorkflowRunStatus `json:"status,omitempty"`
}

// WorkflowRunSpec defines what workflow to execute.
// Exactly one of WorkflowRef or Inline must be specified.
type WorkflowRunSpec struct {
	// WorkflowRef references an existing Workflow template by name.
	// The Workflow must exist in the same namespace as the WorkflowRun.
	// Mutually exclusive with Inline.
	// +optional
	WorkflowRef string `json:"workflowRef,omitempty"`

	// Inline defines the workflow stages directly in the WorkflowRun.
	// Use this for ad-hoc workflows that don't need to be reused.
	// Mutually exclusive with WorkflowRef.
	// +optional
	Inline *WorkflowSpec `json:"inline,omitempty"`
}

// WorkflowRunStatus defines the observed state of WorkflowRun
type WorkflowRunStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase is the current phase of the workflow execution.
	// +optional
	Phase WorkflowPhase `json:"phase,omitempty"`

	// CurrentStage is the index (0-based) of the currently executing stage.
	// -1 means no stage has started yet.
	// +optional
	CurrentStage int32 `json:"currentStage,omitempty"`

	// TotalTasks is the total number of tasks across all stages.
	// +optional
	TotalTasks int32 `json:"totalTasks,omitempty"`

	// CompletedTasks is the number of tasks that have completed successfully.
	// +optional
	CompletedTasks int32 `json:"completedTasks,omitempty"`

	// FailedTasks is the number of tasks that have failed.
	// +optional
	FailedTasks int32 `json:"failedTasks,omitempty"`

	// StartTime is when the workflow started executing.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the workflow finished (success or failure).
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// StageStatuses contains status information for each stage.
	// +optional
	StageStatuses []WorkflowStageStatus `json:"stageStatuses,omitempty"`

	// Conditions represent the latest available observations of the WorkflowRun's state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// WorkflowRunList contains a list of WorkflowRun
type WorkflowRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkflowRun `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced",shortName=cwf
// +kubebuilder:printcolumn:JSONPath=`.spec.schedule`,name="Schedule",type=string
// +kubebuilder:printcolumn:JSONPath=`.spec.suspend`,name="Suspend",type=boolean
// +kubebuilder:printcolumn:JSONPath=`.status.lastScheduleTime`,name="Last Schedule",type=date
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// CronWorkflow represents a scheduled workflow that runs on a cron schedule.
// CronWorkflow creates WorkflowRun resources at scheduled times, similar to how
// Kubernetes CronJob creates Jobs.
type CronWorkflow struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of CronWorkflow
	Spec CronWorkflowSpec `json:"spec"`

	// Status represents the current status of the CronWorkflow
	// +optional
	Status CronWorkflowStatus `json:"status,omitempty"`
}

// CronWorkflowSpec defines the CronWorkflow configuration.
// Exactly one of WorkflowRef or Inline must be specified.
type CronWorkflowSpec struct {
	// Schedule specifies the cron schedule in standard cron format.
	// Example: "0 9 * * *" runs at 9:00 AM every day.
	// +required
	Schedule string `json:"schedule"`

	// WorkflowRef references an existing Workflow template by name.
	// The Workflow must exist in the same namespace as the CronWorkflow.
	// Mutually exclusive with Inline.
	// +optional
	WorkflowRef string `json:"workflowRef,omitempty"`

	// Inline defines the workflow stages directly.
	// Use this for scheduled workflows that don't need a separate Workflow template.
	// Mutually exclusive with WorkflowRef.
	// +optional
	Inline *WorkflowSpec `json:"inline,omitempty"`

	// Suspend tells the controller to suspend subsequent executions.
	// It does not apply to already started executions.
	// Defaults to false.
	// +optional
	Suspend *bool `json:"suspend,omitempty"`
}

// CronWorkflowStatus defines the observed state of CronWorkflow
type CronWorkflowStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Active is a list of references to currently running WorkflowRuns.
	// +optional
	Active []corev1.ObjectReference `json:"active,omitempty"`

	// LastScheduleTime is the last time a WorkflowRun was successfully scheduled.
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`

	// LastSuccessfulTime is the last time a WorkflowRun completed successfully.
	// +optional
	LastSuccessfulTime *metav1.Time `json:"lastSuccessfulTime,omitempty"`

	// Conditions represent the latest available observations of the CronWorkflow's state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CronWorkflowList contains a list of CronWorkflow
type CronWorkflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CronWorkflow `json:"items"`
}
