// Copyright Contributors to the KubeTask project

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConcurrencyPolicy describes how the webhook trigger will handle concurrent tasks.
// +kubebuilder:validation:Enum=Allow;Forbid;Replace
type ConcurrencyPolicy string

const (
	// ConcurrencyPolicyAllow allows multiple tasks to run concurrently.
	ConcurrencyPolicyAllow ConcurrencyPolicy = "Allow"

	// ConcurrencyPolicyForbid ignores new webhooks if there's already a running task.
	ConcurrencyPolicyForbid ConcurrencyPolicy = "Forbid"

	// ConcurrencyPolicyReplace stops the running task and creates a new one.
	ConcurrencyPolicyReplace ConcurrencyPolicy = "Replace"
)

// MatchPolicy describes how the webhook trigger will select matching rules.
// +kubebuilder:validation:Enum=First;All
type MatchPolicy string

const (
	// MatchPolicyFirst triggers only the first matching rule (like Nginx location matching).
	// Rules are evaluated in array order, and the first rule whose filter matches
	// will be used to create a resource. Subsequent rules are not evaluated.
	MatchPolicyFirst MatchPolicy = "First"

	// MatchPolicyAll triggers all matching rules, creating multiple resources.
	// All rules are evaluated, and a resource is created for each rule whose filter matches.
	// This is useful when a single webhook should trigger different workflows.
	MatchPolicyAll MatchPolicy = "All"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced",shortName=wht
// +kubebuilder:printcolumn:JSONPath=`.status.totalTriggered`,name="Triggered",type=integer
// +kubebuilder:printcolumn:JSONPath=`.status.lastTriggeredTime`,name="Last Trigger",type=date
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// WebhookTrigger represents a webhook-to-Task mapping rule.
// When the webhook endpoint receives a request that matches the configured filters,
// it creates a Task based on the taskTemplate.
//
// Each WebhookTrigger has a unique endpoint at:
//
//	/webhooks/<namespace>/<trigger-name>
//
// WebhookTrigger is platform-agnostic and supports any webhook source
// (GitHub, GitLab, custom systems, etc.) through configurable authentication
// and filtering.
type WebhookTrigger struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of WebhookTrigger
	Spec WebhookTriggerSpec `json:"spec"`

	// Status represents the current status of the WebhookTrigger
	// +optional
	Status WebhookTriggerStatus `json:"status,omitempty"`
}

// WebhookTriggerSpec defines the WebhookTrigger configuration
type WebhookTriggerSpec struct {
	// Auth defines authentication for webhook validation.
	// If not specified, webhooks are accepted without authentication.
	// Authentication is applied at the trigger level (before rule evaluation).
	// +optional
	Auth *WebhookAuth `json:"auth,omitempty"`

	// MatchPolicy specifies how matching rules are selected.
	// - First: Only trigger the first matching rule (default)
	// - All: Trigger all matching rules, creating multiple resources
	//
	// This field is only relevant when Rules is specified.
	// +kubebuilder:validation:Enum=First;All
	// +kubebuilder:default=First
	// +optional
	MatchPolicy MatchPolicy `json:"matchPolicy,omitempty"`

	// Rules defines multiple filter-to-resourceTemplate mappings.
	// Each rule specifies a CEL filter and the resource template to use when the filter matches.
	// Rules are evaluated in array order.
	//
	// When matchPolicy is "First", only the first matching rule triggers.
	// When matchPolicy is "All", all matching rules trigger (creating multiple resources).
	//
	// Rules is mutually exclusive with the legacy Filter and TaskTemplate fields.
	// If Rules is specified, Filter and TaskTemplate must be empty.
	// +optional
	// +listType=map
	// +listMapKey=name
	Rules []WebhookRule `json:"rules,omitempty"`

	// Filter is a CEL expression that must evaluate to true for the webhook to trigger.
	// If not specified, all webhooks are accepted.
	//
	// DEPRECATED: Use Rules[].filter instead for multiple filters.
	// This field is kept for backward compatibility with simple single-rule triggers.
	// Mutually exclusive with Rules.
	//
	// The webhook payload is available as the variable "body".
	// HTTP headers are available as the variable "headers" (map[string]string, lowercase keys).
	//
	// CEL provides powerful filtering capabilities:
	//   - Field access: body.action, body.repository.full_name
	//   - Comparisons: body.action == "opened", body.pull_request.additions < 500
	//   - List operations: body.action in ["opened", "synchronize"]
	//   - String functions: body.title.startsWith("[WIP]"), body.branch.matches("^feature/.*")
	//   - Existence checks: has(body.pull_request) && body.pull_request.draft == false
	//   - List predicates: body.labels.exists(l, l.name == "needs-review")
	//
	// Examples:
	//   # Simple equality
	//   filter: 'body.action == "opened"'
	//
	//   # Multiple conditions
	//   filter: 'body.action in ["opened", "synchronize"] && body.repository.full_name == "myorg/myrepo"'
	//
	//   # Complex logic
	//   filter: |
	//     !body.pull_request.title.startsWith("[WIP]") &&
	//     body.pull_request.additions + body.pull_request.deletions < 500 &&
	//     body.pull_request.labels.exists(l, l.name == "needs-review")
	//
	// +optional
	Filter string `json:"filter,omitempty"`

	// ConcurrencyPolicy specifies how to treat concurrent resources triggered by this webhook.
	// This is the default policy applied at the trigger level.
	// Individual rules can override this with their own concurrencyPolicy.
	//
	// - Allow: Create a new resource regardless of existing running resources (default)
	// - Forbid: Skip this webhook if there's already a running resource
	// - Replace: Stop the running resource and create a new one
	// +kubebuilder:validation:Enum=Allow;Forbid;Replace
	// +kubebuilder:default=Allow
	// +optional
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`

	// TaskTemplate defines the Task to create when a webhook matches.
	// DEPRECATED: Use ResourceTemplate instead for new code.
	// This field is kept for backward compatibility with existing triggers.
	// Mutually exclusive with Rules and ResourceTemplate.
	// +optional
	TaskTemplate *WebhookTaskTemplate `json:"taskTemplate,omitempty"`

	// ResourceTemplate defines what resource to create when a webhook matches.
	// Supports Task, WorkflowRun (inline), or WorkflowRef.
	// This replaces TaskTemplate for new code and allows triggering Workflows.
	// Mutually exclusive with Rules and TaskTemplate.
	// +optional
	ResourceTemplate *WebhookResourceTemplate `json:"resourceTemplate,omitempty"`
}

// WebhookAuth defines authentication configuration for webhook validation.
// Exactly one authentication method should be specified.
type WebhookAuth struct {
	// HMAC configures HMAC signature validation.
	// Common for GitHub (X-Hub-Signature-256) and similar platforms.
	// +optional
	HMAC *HMACAuth `json:"hmac,omitempty"`

	// BearerToken configures Bearer token validation from Authorization header.
	// +optional
	BearerToken *BearerTokenAuth `json:"bearerToken,omitempty"`

	// Header configures simple header value matching.
	// Common for GitLab (X-Gitlab-Token) and custom webhooks.
	// +optional
	Header *HeaderAuth `json:"header,omitempty"`
}

// HMACAuth configures HMAC signature validation.
// The signature is computed over the request body and compared
// with the value in the specified header.
type HMACAuth struct {
	// SecretRef references a Secret containing the HMAC secret key.
	// +required
	SecretRef SecretKeyReference `json:"secretRef"`

	// SignatureHeader is the HTTP header name containing the signature.
	// Example: "X-Hub-Signature-256" for GitHub, "X-Signature" for custom systems.
	// +required
	SignatureHeader string `json:"signatureHeader"`

	// Algorithm specifies the HMAC algorithm to use.
	// +kubebuilder:validation:Enum=sha1;sha256;sha512
	// +kubebuilder:default=sha256
	// +optional
	Algorithm string `json:"algorithm,omitempty"`
}

// BearerTokenAuth configures Bearer token validation.
// Expects the Authorization header in format: "Bearer <token>"
type BearerTokenAuth struct {
	// SecretRef references a Secret containing the expected token.
	// +required
	SecretRef SecretKeyReference `json:"secretRef"`
}

// HeaderAuth configures simple header value matching.
// The specified header's value must exactly match the secret value.
type HeaderAuth struct {
	// Name is the HTTP header name to check.
	// Example: "X-Gitlab-Token", "X-Custom-Auth"
	// +required
	Name string `json:"name"`

	// SecretRef references a Secret containing the expected header value.
	// +required
	SecretRef SecretKeyReference `json:"secretRef"`
}

// SecretKeyReference references a specific key within a Secret.
type SecretKeyReference struct {
	// Name of the Secret.
	// +required
	Name string `json:"name"`

	// Key of the Secret to select.
	// +required
	Key string `json:"key"`
}

// WebhookTaskTemplate defines the Task to create when a webhook matches.
// The description field supports Go template syntax with the webhook payload
// available as the template data.
//
// DEPRECATED: Use WebhookResourceTemplate with Task field for new code.
// This type is kept for backward compatibility.
type WebhookTaskTemplate struct {
	// AgentRef references an Agent for task execution.
	// If not specified, uses the "default" Agent in the same namespace.
	// +optional
	AgentRef string `json:"agentRef,omitempty"`

	// Description is the task instruction/prompt.
	// Supports Go template syntax with webhook payload data.
	//
	// Available template data:
	//   - The entire webhook JSON payload is available as the root object
	//   - Example: {{ .pull_request.number }}, {{ .repository.full_name }}
	//
	// Example:
	//   description: |
	//     Review Pull Request #{{ .pull_request.number }}
	//     Repository: {{ .repository.full_name }}
	//     Author: {{ .pull_request.user.login }}
	// +required
	Description string `json:"description"`

	// Contexts provides additional context for the task.
	// Each context can be a reference to a Context CRD (via Ref) or inline definition.
	// +optional
	Contexts []ContextSource `json:"contexts,omitempty"`
}

// WebhookRule defines a single rule within a WebhookTrigger.
// Each rule has its own filter and resourceTemplate, allowing different
// webhook payloads to trigger different resources from the same URL.
type WebhookRule struct {
	// Name is a unique identifier for this rule within the trigger.
	// Used for status tracking, logging, debugging, and resource naming.
	// Must be unique within the rules array.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// Filter is a CEL expression that must evaluate to true for this rule to trigger.
	// If not specified, this rule matches all webhooks (after authentication).
	//
	// The webhook payload is available as the variable "body".
	// HTTP headers are available as the variable "headers" (map[string]string, lowercase keys).
	//
	// Examples:
	//   # Match GitHub PR opened events
	//   filter: 'body.action == "opened" && has(body.pull_request)'
	//
	//   # Match specific event types via headers
	//   filter: 'headers["x-github-event"] == "push"'
	//
	// +optional
	Filter string `json:"filter,omitempty"`

	// ConcurrencyPolicy overrides the trigger-level concurrencyPolicy for this rule.
	// If not specified, the trigger-level concurrencyPolicy is used.
	//
	// This allows fine-grained control: some rules might use "Allow" to create
	// multiple resources, while others use "Replace" to stop existing resources.
	//
	// Note: When matchPolicy is "All" and concurrencyPolicy is "Replace",
	// each rule tracks its own active resources independently.
	// +kubebuilder:validation:Enum=Allow;Forbid;Replace
	// +optional
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`

	// ResourceTemplate defines what resource to create when this rule's filter matches.
	// +required
	ResourceTemplate WebhookResourceTemplate `json:"resourceTemplate"`
}

// WebhookResourceTemplate defines the resource to create when a webhook matches.
// Supports Task, WorkflowRun (inline), or WorkflowRef.
// Exactly one of Task, WorkflowRun, or WorkflowRef must be specified.
type WebhookResourceTemplate struct {
	// Task defines a Task to create.
	// Use this for simple single-stage task execution.
	// Mutually exclusive with WorkflowRun and WorkflowRef.
	// +optional
	Task *WebhookTaskSpec `json:"task,omitempty"`

	// WorkflowRun defines a WorkflowRun to create with inline workflow definition.
	// Use this for multi-stage workflows defined directly in the trigger.
	// Mutually exclusive with Task and WorkflowRef.
	// +optional
	WorkflowRun *WebhookWorkflowRunSpec `json:"workflowRun,omitempty"`

	// WorkflowRef references an existing Workflow by name.
	// Creates a WorkflowRun that references this Workflow.
	// Use this to reuse pre-defined Workflow templates.
	// Mutually exclusive with Task and WorkflowRun.
	// +optional
	WorkflowRef string `json:"workflowRef,omitempty"`
}

// WebhookTaskSpec defines Task-specific fields for webhook resource templates.
// This is similar to WebhookTaskTemplate but used within WebhookResourceTemplate.
type WebhookTaskSpec struct {
	// AgentRef references an Agent for task execution.
	// If not specified, uses the "default" Agent in the same namespace.
	// +optional
	AgentRef string `json:"agentRef,omitempty"`

	// Description is the task instruction/prompt.
	// Supports Go template syntax with webhook payload data.
	//
	// Available template data:
	//   - The entire webhook JSON payload is available as the root object
	//   - HTTP headers are available via .headers (map with lowercase keys)
	//   - Example: {{ .pull_request.number }}, {{ .repository.full_name }}
	// +required
	Description string `json:"description"`

	// Contexts provides additional context for the task.
	// Each context can be a reference to a Context CRD (via Ref) or inline definition.
	// +optional
	Contexts []ContextSource `json:"contexts,omitempty"`
}

// WebhookWorkflowRunSpec defines WorkflowRun-specific fields for webhook resource templates.
// Exactly one of WorkflowRef or Inline must be specified.
type WebhookWorkflowRunSpec struct {
	// WorkflowRef references an existing Workflow by name.
	// The Workflow must exist in the same namespace as the WebhookTrigger.
	// Mutually exclusive with Inline.
	// +optional
	WorkflowRef string `json:"workflowRef,omitempty"`

	// Inline defines workflow stages directly in the trigger.
	// Use this for ad-hoc workflows that don't need to be reused.
	// Stage task descriptions support Go template syntax with webhook payload data.
	// Mutually exclusive with WorkflowRef.
	// +optional
	Inline *WorkflowSpec `json:"inline,omitempty"`
}

// WebhookTriggerStatus defines the observed state of WebhookTrigger
type WebhookTriggerStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LastTriggeredTime is when the webhook was last triggered successfully.
	// +optional
	LastTriggeredTime *metav1.Time `json:"lastTriggeredTime,omitempty"`

	// TotalTriggered is the total number of times this trigger created a resource.
	// +optional
	TotalTriggered int64 `json:"totalTriggered,omitempty"`

	// ActiveTasks lists the names of currently running Tasks created by this trigger.
	// DEPRECATED: Use ActiveResources instead for new code.
	// Used for concurrency policy enforcement with legacy TaskTemplate.
	// +optional
	ActiveTasks []string `json:"activeTasks,omitempty"`

	// ActiveResources lists the names of currently running resources (Task or WorkflowRun)
	// created by this trigger. Used for concurrency policy enforcement.
	// When using Rules, this contains resources from all rules.
	// For per-rule tracking, see RuleStatuses[].ActiveResources.
	// +optional
	ActiveResources []string `json:"activeResources,omitempty"`

	// RuleStatuses contains per-rule status information.
	// Only populated when Rules is used (not for legacy TaskTemplate triggers).
	// +optional
	// +listType=map
	// +listMapKey=name
	RuleStatuses []WebhookRuleStatus `json:"ruleStatuses,omitempty"`

	// WebhookURL is the full URL path for this webhook endpoint.
	// Format: /webhooks/<namespace>/<trigger-name>
	// +optional
	WebhookURL string `json:"webhookURL,omitempty"`

	// Conditions represent the latest available observations of the WebhookTrigger's state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// WebhookRuleStatus contains status information for a single rule.
type WebhookRuleStatus struct {
	// Name is the rule name (matches WebhookRule.Name).
	// +required
	Name string `json:"name"`

	// LastTriggeredTime is when this rule was last triggered.
	// +optional
	LastTriggeredTime *metav1.Time `json:"lastTriggeredTime,omitempty"`

	// TotalTriggered is the number of times this rule created a resource.
	// +optional
	TotalTriggered int64 `json:"totalTriggered,omitempty"`

	// ActiveResources lists the names of currently running resources created by this rule.
	// Used for per-rule concurrency policy enforcement.
	// +optional
	ActiveResources []string `json:"activeResources,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// WebhookTriggerList contains a list of WebhookTrigger
type WebhookTriggerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WebhookTrigger `json:"items"`
}
