// Copyright Contributors to the KubeTask project

// Package v1alpha1 contains the v1alpha1 API definitions
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope="Namespaced"
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// Batch defines a task batch template that specifies WHAT to do and WHERE to do it.
// Batch is AI-agnostic and works with any agent.
type Batch struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of Batch
	Spec BatchSpec `json:"spec"`
}

// BatchSpec defines the task batch template with common and variable contexts
type BatchSpec struct {
	// CommonContext - contexts shared across all tasks (the constant part)
	// These contexts are included in every task
	// +required
	CommonContext []Context `json:"commonContext"`

	// VariableContexts - variable contexts that differ per task (the variable part)
	// Each item is a list of contexts for one task
	// Task[i] = commonContext (constant) + variableContexts[i] (variable)
	// Total tasks = len(variableContexts)
	//
	// Example:
	//   commonContext: [task.md, guide.md]  // constant - same for all tasks
	//   variableContexts: [                 // variable - different per task
	//     [repo1:main, repo1-config.json],  // task 1's variable part
	//     [repo2:main, repo2-config.json],  // task 2's variable part
	//   ]
	//   Generates 2 tasks:
	//     Task 1 = [task.md, guide.md] + [repo1:main, repo1-config.json]
	//     Task 2 = [task.md, guide.md] + [repo2:main, repo2-config.json]
	//
	// +required
	VariableContexts []ContextSet `json:"variableContexts"`

	// WorkspaceConfigRef references a WorkspaceConfig for this Batch.
	// If not specified, uses the "default" WorkspaceConfig in the same namespace.
	// +optional
	WorkspaceConfigRef string `json:"workspaceConfigRef,omitempty"`
}

// ContextSet represents a set of contexts for one task
type ContextSet []Context

// ContextType defines the type of context
// +kubebuilder:validation:Enum=File;RemoteFile
type ContextType string

const (
	// ContextTypeFile represents a file context (task.md, guide.md, etc.)
	ContextTypeFile ContextType = "File"

	// ContextTypeRemoteFile represents a remote file context fetched via HTTP/HTTPS.
	// Use this for files that need to be fetched at runtime (e.g., from GitHub raw URLs).
	// The content is fetched fresh each time the task runs.
	ContextTypeRemoteFile ContextType = "RemoteFile"

	// Future context types:
	// ContextTypeAPI        ContextType = "API"
	// ContextTypeDatabase   ContextType = "Database"
	// ContextTypeCloudResource ContextType = "CloudResource"
)

// Context represents different types of task inputs
// This is a polymorphic type that can represent File, API, Database, etc.
type Context struct {
	// Type of context: File, RemoteFile, API, Database, etc.
	// +required
	Type ContextType `json:"type"`

	// File context (required when Type == "File")
	// +optional
	File *FileContext `json:"file,omitempty"`

	// RemoteFile context (required when Type == "RemoteFile")
	// +optional
	RemoteFile *RemoteFileContext `json:"remoteFile,omitempty"`

	// Future context types can be added here:
	// API *APIContext `json:"api,omitempty"`
	// Database *DatabaseContext `json:"database,omitempty"`
}

// FileContext represents a file with content from various sources
type FileContext struct {
	// File name (e.g., "task.md", "config.json")
	// +required
	Name string `json:"name"`

	// File content source (exactly one must be specified)
	// +required
	Source FileSource `json:"source"`

	// MountPath specifies where to mount this file in the agent pod.
	// If not specified, the file content will be aggregated into /workspace/task.md
	// along with other contexts that don't have a mountPath specified.
	// +optional
	MountPath *string `json:"mountPath,omitempty"`
}

// RemoteFileContext represents a file fetched from a remote URL at runtime.
// This is useful for files that may be updated frequently, such as files from
// GitHub repositories that need to be fetched fresh each time a task runs.
type RemoteFileContext struct {
	// Name is the filename to use when saving the fetched content.
	// (e.g., "CLAUDE.md", "config.json")
	// +required
	Name string `json:"name"`

	// URL is the HTTP/HTTPS URL to fetch the file from.
	// For GitHub files, use raw URLs like:
	//   https://raw.githubusercontent.com/owner/repo/branch/path/to/file
	// +required
	URL string `json:"url"`

	// Headers specifies optional HTTP headers to include in the request.
	// Useful for authentication (e.g., Authorization header for private repos).
	// +optional
	Headers []HTTPHeader `json:"headers,omitempty"`

	// MountPath specifies where to mount this file in the agent pod.
	// If not specified, the file content will be aggregated into /workspace/task.md
	// along with other contexts that don't have a mountPath specified.
	// +optional
	MountPath *string `json:"mountPath,omitempty"`
}

// HTTPHeader represents an HTTP header key-value pair
type HTTPHeader struct {
	// Name is the header name (e.g., "Authorization", "Accept")
	// +required
	Name string `json:"name"`

	// Value is the header value.
	// For sensitive values, use ValueFrom instead.
	// +optional
	Value string `json:"value,omitempty"`

	// ValueFrom references a secret for sensitive header values.
	// +optional
	ValueFrom *SecretKeySelector `json:"valueFrom,omitempty"`
}

// FileSource represents a source for file content
type FileSource struct {
	// Inline content
	// +optional
	Inline *string `json:"inline,omitempty"`

	// Reference to a key in a ConfigMap
	// +optional
	ConfigMapKeyRef *ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`

	// Reference to a key in a Secret
	// +optional
	SecretKeyRef *SecretKeySelector `json:"secretKeyRef,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// BatchList contains a list of Batch
type BatchList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Batch `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced"
// +kubebuilder:printcolumn:JSONPath=`.spec.batchRef`,name="Batch",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.phase`,name="Phase",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.progress.completed`,name="Completed",type=integer
// +kubebuilder:printcolumn:JSONPath=`.status.progress.total`,name="Total",type=integer
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// BatchRun represents a specific execution instance of a Batch.
// Each execution creates a new BatchRun.
type BatchRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of BatchRun
	Spec BatchRunSpec `json:"spec"`

	// Status represents the current status of the BatchRun
	// +optional
	Status BatchRunStatus `json:"status,omitempty"`
}

// BatchRunSpec defines the BatchRun configuration
// Similar to Tekton PipelineRun, supports both reference and inline definition
type BatchRunSpec struct {
	// Reference to an existing Batch
	// +optional
	BatchRef string `json:"batchRef,omitempty"`

	// Inline Batch definition (alternative to BatchRef)
	// Allows defining the batch directly without creating a separate Batch resource
	// +optional
	BatchSpec *BatchSpec `json:"batchSpec,omitempty"`
}

// BatchRunPhase represents the current phase of a BatchRun
// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed
type BatchRunPhase string

const (
	// BatchRunPhasePending means the BatchRun has been created but not yet started
	BatchRunPhasePending BatchRunPhase = "Pending"
	// BatchRunPhaseRunning means the BatchRun is currently executing tasks
	BatchRunPhaseRunning BatchRunPhase = "Running"
	// BatchRunPhaseSucceeded means all tasks completed successfully
	BatchRunPhaseSucceeded BatchRunPhase = "Succeeded"
	// BatchRunPhaseFailed means one or more tasks failed
	BatchRunPhaseFailed BatchRunPhase = "Failed"
)

// BatchRunStatus defines the observed state of BatchRun
type BatchRunStatus struct {
	// Execution phase
	// +optional
	Phase BatchRunPhase `json:"phase,omitempty"`

	// Start time
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Completion time
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Progress statistics
	// +optional
	Progress ProgressStatus `json:"progress,omitempty"`

	// Task details list
	// +optional
	Tasks []TaskStatus `json:"tasks,omitempty"`

	// Kubernetes standard conditions
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ProgressStatus tracks execution progress
type ProgressStatus struct {
	// Total number of tasks
	// +optional
	Total int `json:"total,omitempty"`

	// Number of pending tasks
	// +optional
	Pending int `json:"pending,omitempty"`

	// Number of running tasks
	// +optional
	Running int `json:"running,omitempty"`

	// Number of completed tasks
	// +optional
	Completed int `json:"completed,omitempty"`

	// Number of failed tasks
	// +optional
	Failed int `json:"failed,omitempty"`
}

// TaskPhase represents the current phase of a task
// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed
type TaskPhase string

const (
	// TaskPhasePending means the task has not started yet
	TaskPhasePending TaskPhase = "Pending"
	// TaskPhaseRunning means the task is currently executing
	TaskPhaseRunning TaskPhase = "Running"
	// TaskPhaseSucceeded means the task completed successfully
	TaskPhaseSucceeded TaskPhase = "Succeeded"
	// TaskPhaseFailed means the task failed
	TaskPhaseFailed TaskPhase = "Failed"
)

// TaskStatus represents the status of a single task
type TaskStatus struct {
	// Task contexts (commonContext + one target)
	// This is what makes this task unique
	// +required
	Contexts []Context `json:"contexts"`

	// Task status
	// +required
	Status TaskPhase `json:"status"`

	// Kubernetes Job name
	// +optional
	JobName string `json:"jobName,omitempty"`

	// Start time
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Completion time
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// BatchRunList contains a list of BatchRun
type BatchRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BatchRun `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced"
// +kubebuilder:printcolumn:JSONPath=`.status.phase`,name="Phase",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.jobName`,name="Job",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// Task represents a single task execution.
// Unlike BatchRun which manages multiple tasks, Task is for simple one-off executions.
// Task is a simplified API for users who want to execute a single task without creating a Batch.
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

	// WorkspaceConfigRef references a WorkspaceConfig for this task.
	// If not specified, uses the "default" WorkspaceConfig in the same namespace.
	// +optional
	WorkspaceConfigRef string `json:"workspaceConfigRef,omitempty"`
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

// WorkspaceConfig defines the workspace environment for task execution.
// Workspace = AI agent + permissions + tools + infrastructure
// This is the execution black box - Batch creators don't need to understand execution details.
type WorkspaceConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the workspace configuration
	Spec WorkspaceConfigSpec `json:"spec"`
}

// WorkspaceConfigSpec defines workspace configuration
type WorkspaceConfigSpec struct {
	// Agent container image to use for task execution.
	// The controller generates Jobs with this image.
	// If not specified, defaults to "quay.io/zhaoxue/kubetask-agent:latest".
	// +optional
	AgentImage string `json:"agentImage,omitempty"`

	// Tools container image that provides CLI tools (git, gh, kubectl, etc.)
	// for the agent to use during task execution.
	//
	// The tools image must follow the standard directory structure:
	//   /tools/bin/  - Executables (added to PATH)
	//   /tools/lib/  - Shared libraries and runtimes (e.g., node_modules)
	//
	// When specified, the controller creates an initContainer that copies
	// /tools from this image to a shared volume, making the tools available
	// to the agent container via PATH environment variable.
	//
	// This enables decoupling of agent and tools - each can be built and
	// versioned independently, and combined at runtime.
	// +optional
	ToolsImage string `json:"toolsImage,omitempty"`

	// DefaultContexts defines the base-level contexts that are included in all tasks
	// using this WorkspaceConfig. These contexts are applied at the lowest priority,
	// meaning Batch commonContext and variableContexts take precedence.
	//
	// Context priority (lowest to highest):
	//   1. WorkspaceConfig.defaultContexts (base layer)
	//   2. Batch.commonContext (shared across all tasks in the batch)
	//   3. Batch.variableContexts[i] (task-specific contexts)
	//
	// Use this for organization-wide defaults like coding standards, security policies,
	// or common tool configurations that should apply to all tasks.
	// +optional
	DefaultContexts []Context `json:"defaultContexts,omitempty"`
}

// SecretKeySelector selects a key of a Secret.
type SecretKeySelector struct {
	// Name of the secret
	// +required
	Name string `json:"name"`

	// Key of the secret to select from
	// +required
	Key string `json:"key"`

	// Specify whether the Secret must be defined
	// +optional
	Optional *bool `json:"optional,omitempty"`
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

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// WorkspaceConfigList contains a list of WorkspaceConfig
type WorkspaceConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkspaceConfig `json:"items"`
}
