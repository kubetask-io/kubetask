// Copyright Contributors to the KubeOpenCode project

package types

import (
	"time"
)

// ServerInfo represents server information
type ServerInfo struct {
	Version string `json:"version"`
}

// NamespaceList represents a list of namespaces
type NamespaceList struct {
	Namespaces []string `json:"namespaces"`
}

// AgentReference represents a reference to an Agent
type AgentReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// ContextItem represents a context item in the API
type ContextItem struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	MountPath   string `json:"mountPath,omitempty"`
}

// CreateTaskRequest represents a request to create a task
type CreateTaskRequest struct {
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description"`
	AgentRef    *AgentReference `json:"agentRef,omitempty"`
	Contexts    []ContextItem   `json:"contexts,omitempty"`
}

// TaskResponse represents a task in API responses
type TaskResponse struct {
	Name           string          `json:"name"`
	Namespace      string          `json:"namespace"`
	Phase          string          `json:"phase"`
	Description    string          `json:"description,omitempty"`
	AgentRef       *AgentReference `json:"agentRef,omitempty"`
	PodName        string          `json:"podName,omitempty"`
	PodNamespace   string          `json:"podNamespace,omitempty"`
	StartTime      *time.Time      `json:"startTime,omitempty"`
	CompletionTime *time.Time      `json:"completionTime,omitempty"`
	Duration       string          `json:"duration,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	Conditions     []Condition     `json:"conditions,omitempty"`
}

// TaskListResponse represents a list of tasks
type TaskListResponse struct {
	Tasks []TaskResponse `json:"tasks"`
	Total int            `json:"total"`
}

// Condition represents a status condition
type Condition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// CredentialInfo represents credential information (without secrets)
type CredentialInfo struct {
	Name      string `json:"name"`
	SecretRef string `json:"secretRef"`
	MountPath string `json:"mountPath,omitempty"`
	Env       string `json:"env,omitempty"`
}

// QuotaInfo represents quota configuration
type QuotaInfo struct {
	MaxTaskStarts int32 `json:"maxTaskStarts,omitempty"`
	WindowSeconds int32 `json:"windowSeconds,omitempty"`
}

// AgentResponse represents an agent in API responses
type AgentResponse struct {
	Name               string           `json:"name"`
	Namespace          string           `json:"namespace"`
	ExecutorImage      string           `json:"executorImage,omitempty"`
	AgentImage         string           `json:"agentImage,omitempty"`
	WorkspaceDir       string           `json:"workspaceDir,omitempty"`
	ContextsCount      int              `json:"contextsCount"`
	CredentialsCount   int              `json:"credentialsCount"`
	MaxConcurrentTasks *int32           `json:"maxConcurrentTasks,omitempty"`
	Quota              *QuotaInfo       `json:"quota,omitempty"`
	AllowedNamespaces  []string         `json:"allowedNamespaces,omitempty"`
	Credentials        []CredentialInfo `json:"credentials,omitempty"`
	Contexts           []ContextItem    `json:"contexts,omitempty"`
	CreatedAt          time.Time        `json:"createdAt"`
}

// AgentListResponse represents a list of agents
type AgentListResponse struct {
	Agents []AgentResponse `json:"agents"`
	Total  int             `json:"total"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	Code    int    `json:"code"`
}
