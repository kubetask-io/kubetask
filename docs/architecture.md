# KubeTask Architecture & API Design

## Table of Contents

1. [System Overview](#system-overview)
2. [API Design](#api-design)
3. [System Architecture](#system-architecture)
4. [Custom Resource Definitions](#custom-resource-definitions)
5. [Agent Configuration](#agent-configuration)
6. [System Configuration](#system-configuration)
7. [Complete Examples](#complete-examples)
8. [kubectl Usage](#kubectl-usage)

---

## System Overview

KubeTask is a Kubernetes-native system that executes AI-powered tasks using Custom Resources (CRs) and the Operator pattern. It provides a simple, declarative way to run AI agents (like Claude, Gemini) as Kubernetes Jobs.

### Core Goals

- Use Kubernetes CRDs to define Task resources
- Use Controller pattern to manage resource lifecycle
- Execute tasks as Kubernetes Jobs
- No external databases or message queues required
- Seamless integration with Kubernetes ecosystem

### Key Advantages

- **Simplified Architecture**: No PostgreSQL, Redis - reduced component dependencies
- **Native Integration**: Works seamlessly with Helm, Kustomize, ArgoCD and other K8s tools
- **Declarative Management**: Use K8s resource definitions, supports GitOps
- **Infrastructure Reuse**: Logs, monitoring, auth/authz all leverage K8s capabilities
- **Simplified Operations**: Manage with standard K8s tools (kubectl, dashboard)
- **Batch Operations**: Use Helm/Kustomize to create multiple Tasks (Kubernetes-native approach)

---

## API Design

### Resource Overview

| Resource | Purpose | Stability |
|----------|---------|-----------|
| **Task** | Single task execution (primary API) | Stable - semantic name |
| **Workflow** | Multi-stage task template (no execution) | Stable - reusable template |
| **WorkflowRun** | Workflow execution instance | Stable - DAG-like execution |
| **CronWorkflow** | Scheduled WorkflowRun triggering | Stable - follows K8s CronJob pattern |
| **WebhookTrigger** | Webhook-driven Task creation | Stable - event-driven triggering |
| **Context** | Reusable context for AI agents (KNOW) | Stable - Context Engineering support |
| **Agent** | AI agent configuration (HOW to execute) | Stable - independent of project name |
| **KubeTaskConfig** | System-level configuration (TTL, lifecycle) | Stable - system settings |

### Key Design Decisions

#### 1. Task as Primary API

**Rationale**: Simple, focused API for single task execution. For batch operations, use Helm/Kustomize to create multiple Tasks.

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Task
```

#### 2. Agent (not KubeTaskConfig)

**Rationale**:
- **Stable**: Independent of project name - won't change even if project renames
- **Semantic**: Reflects architecture philosophy: "Agent = AI + permissions + tools"
- **Clear**: Configures the agent environment for task execution

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Agent
```

#### 3. No Batch/BatchRun

**Rationale**: Kubernetes-native approach - use Helm, Kustomize, or other templating tools to create multiple Tasks. This:
- Reduces API complexity
- Leverages existing Kubernetes tooling
- Follows cloud-native best practices

#### 4. No Retry Mechanism

**Rationale**: AI tasks are fundamentally different from traditional functions:

- **Non-deterministic output**: AI agents may produce different results on each run
- **Non-idempotent operations**: Tasks may perform actions (create PRs, modify files, send messages) that should not be repeated
- **Compound failures**: Retrying a partially completed task may cause duplicate operations or inconsistent state

**Implementation**:
- Jobs are created with `backoffLimit: 0` (no Pod retry on failure)
- Pods use `restartPolicy: Never` (no container restart on failure)
- Task fails immediately when the agent container exits with non-zero code

**If retry is needed**, use external Kubernetes ecosystem components:
- **Argo Workflows**: DAG-based workflow with conditional retry logic
- **Tekton Pipelines**: CI/CD pipelines with result-based retry
- **Custom controllers**: Monitor Task status and create new Tasks based on validation results
- **CronWorkflow**: For periodic re-execution of tasks

This approach:
- Keeps the Task API simple and focused
- Delegates retry/orchestration to specialized tools
- Allows users to implement custom validation before retry
- Prevents accidental duplicate operations from AI agents

**If retry or scheduled execution is needed:**
- **CronWorkflow**: For periodic re-execution of workflows (single-task or multi-task)
- **Argo Workflows**: DAG-based workflow with conditional retry logic
- **Tekton Pipelines**: CI/CD pipelines with result-based retry

### Resource Hierarchy

```
Task (single task execution)
├── TaskSpec
│   ├── description: *string         (syntactic sugar for /workspace/task.md)
│   ├── contexts: []ContextSource    (reference or inline contexts)
│   └── agentRef: string
└── TaskExecutionStatus
    ├── phase: TaskPhase
    ├── jobName: string
    ├── startTime: Time
    └── completionTime: Time

Context (reusable context resource)
└── ContextSpec
    ├── type: ContextType (Text, ConfigMap, Git, Runtime)
    ├── text: string
    ├── configMap: *ConfigMapContext
    ├── git: *GitContext
    └── runtime: *RuntimeContext

Workflow (template only - no Status)
└── WorkflowSpec
    └── stages: []WorkflowStage
        ├── name: string (optional, auto-generated as "stage-0", "stage-1")
        └── tasks: []WorkflowTask
            ├── name: string
            └── spec: TaskSpec

WorkflowRun (execution instance)
├── WorkflowRunSpec
│   ├── workflowRef: string          (mutually exclusive with inline)
│   └── inline: *WorkflowSpec        (mutually exclusive with workflowRef)
└── WorkflowRunStatus
    ├── phase: WorkflowPhase (Pending|Running|Completed|Failed)
    ├── currentStage: int32
    ├── totalTasks: int32
    ├── completedTasks: int32
    ├── failedTasks: int32
    ├── startTime: *Time
    ├── completionTime: *Time
    ├── stageStatuses: []WorkflowStageStatus
    └── conditions: []Condition

CronWorkflow (scheduled WorkflowRun triggering)
├── CronWorkflowSpec
│   ├── schedule: string (cron expression)
│   ├── suspend: *bool
│   ├── workflowRef: string          (mutually exclusive with inline)
│   └── inline: *WorkflowSpec        (mutually exclusive with workflowRef)
└── CronWorkflowStatus
    ├── active: []ObjectReference
    ├── lastScheduleTime: *Time
    ├── lastSuccessfulTime: *Time
    └── conditions: []Condition

WebhookTrigger (webhook-driven Task/WorkflowRun creation)
├── WebhookTriggerSpec
│   ├── auth: *WebhookAuth           (HMAC, BearerToken, or Header)
│   ├── matchPolicy: MatchPolicy     (First or All, for rules-based triggers)
│   ├── rules: []WebhookRule         (multiple filter-to-resourceTemplate mappings)
│   │   ├── name: string             (unique rule identifier)
│   │   ├── filter: string           (CEL expression for filtering)
│   │   ├── concurrencyPolicy: string (per-rule override)
│   │   └── resourceTemplate: WebhookResourceTemplate
│   │       ├── task: *WebhookTaskSpec       (create Task)
│   │       ├── workflowRef: string          (reference Workflow, create WorkflowRun)
│   │       └── workflowRun: *WebhookWorkflowRunSpec (inline WorkflowRun)
│   ├── filter: string               (DEPRECATED: use rules[].filter)
│   ├── concurrencyPolicy: string    (Allow, Forbid, Replace)
│   ├── taskTemplate: *WebhookTaskTemplate   (DEPRECATED: use resourceTemplate)
│   └── resourceTemplate: *WebhookResourceTemplate (new: supports Task/WorkflowRun)
└── WebhookTriggerStatus
    ├── lastTriggeredTime: *Time
    ├── totalTriggered: int64
    ├── activeTasks: []string        (DEPRECATED: use activeResources)
    ├── activeResources: []string    (currently running Tasks/WorkflowRuns)
    ├── ruleStatuses: []WebhookRuleStatus  (per-rule status)
    │   ├── name: string
    │   ├── lastTriggeredTime: *Time
    │   ├── totalTriggered: int64
    │   └── activeResources: []string
    ├── webhookURL: string
    └── conditions: []Condition

Agent (execution configuration)
└── AgentSpec
    ├── agentImage: string
    ├── workspaceDir: string         (default: "/workspace")
    ├── command: []string
    ├── contexts: []ContextSource    (reference or inline contexts)
    ├── credentials: []Credential
    ├── podSpec: *AgentPodSpec
    ├── serviceAccountName: string
    └── maxConcurrentTasks: *int32   (limit concurrent Tasks, nil/0 = unlimited)

KubeTaskConfig (system configuration)
└── KubeTaskConfigSpec
    ├── taskLifecycle: *TaskLifecycleConfig
    │   └── ttlSecondsAfterFinished: *int32
    └── sessionPVC: *SessionPVCConfig
        ├── name: string
        ├── storageClassName: *string
        ├── storageSize: string
        └── retentionPolicy: *SessionRetentionPolicy
            └── ttlSecondsAfterTaskDeletion: *int32
```

### Workflow Template/Instance Pattern

The Workflow API follows a template/instance pattern similar to Kubernetes Deployment/ReplicaSet:

```
Workflow (template - no execution)
    │
    ├──── WorkflowRun (manual execution)
    │         spec.workflowRef: my-workflow
    │         └── Task, Task, Task (child resources)
    │
    ├──── WorkflowRun (inline definition)
    │         spec.inline: { stages: [...] }
    │         └── Task, Task (child resources)
    │
    └──── CronWorkflow (scheduled execution)
              spec.schedule: "0 9 * * *"
              spec.workflowRef: my-workflow
              └── WorkflowRun → Task, Task (created on schedule)
```

**Benefits:**
- Workflow templates are reusable across multiple executions
- WorkflowRun preserves execution history
- CronWorkflow provides scheduled workflow execution
- Clear separation of concerns: definition vs execution vs scheduling

### Complete Type Definitions

```go
// Task represents a single task execution
type Task struct {
    Spec   TaskSpec
    Status TaskExecutionStatus
}

type TaskSpec struct {
    Description *string          // Syntactic sugar for /workspace/task.md
    Contexts    []ContextSource  // Reference or inline contexts
    AgentRef    string           // Reference to Agent
}

// ContextSource can be either a reference to a Context CRD or an inline definition
type ContextSource struct {
    Ref    *ContextRef  // Reference to a Context CRD
    Inline *ContextItem // Inline context definition
}

// ContextRef references a Context CRD and specifies how to mount it
type ContextRef struct {
    Name      string // Name of the Context
    Namespace string // Optional, defaults to Task's namespace
    MountPath string // Empty = append to /workspace/task.md with XML tags
}

// ContextItem defines inline context content
type ContextItem struct {
    Type      ContextType       // Text, ConfigMap, Git, or Runtime
    MountPath string            // Empty = append to /workspace/task.md (ignored for Runtime)
    Text      string            // Content when Type is Text
    ConfigMap *ConfigMapContext // ConfigMap when Type is ConfigMap
    Git       *GitContext       // Git repo when Type is Git
    Runtime   *RuntimeContext   // Platform awareness when Type is Runtime
}

type TaskExecutionStatus struct {
    Phase          TaskPhase
    JobName        string
    StartTime      *metav1.Time
    CompletionTime *metav1.Time
    SessionPodName string         // Name of session Pod (when resume triggered)
    SessionStatus  *SessionStatus // Session state (when persistence enabled)
    Conditions     []metav1.Condition
}

// SessionStatus tracks session Pod state
type SessionStatus struct {
    Phase         SessionPhase // Pending | Active | Terminated
    StartTime     *metav1.Time
    WorkspacePath string       // Path on PVC: /<namespace>/<task-name>/
}

type SessionPhase string
const (
    SessionPhasePending    SessionPhase = "Pending"
    SessionPhaseActive     SessionPhase = "Active"
    SessionPhaseTerminated SessionPhase = "Terminated"
)

// Workflow represents a reusable workflow template (no execution, no Status)
type Workflow struct {
    Spec WorkflowSpec  // No Status - Workflow is a template
}

type WorkflowSpec struct {
    Stages []WorkflowStage // Sequential stages (stage N+1 starts after stage N completes)
}

type WorkflowStage struct {
    Name  string         // Optional, auto-generated as "stage-0", "stage-1" if not specified
    Tasks []WorkflowTask // Tasks to run in parallel within this stage
}

type WorkflowTask struct {
    Name string   // Unique name within workflow (Task CR name = "{workflowrun}-{name}")
    Spec TaskSpec // TaskSpec for the created Task
}

// WorkflowRun represents an execution instance of a Workflow
type WorkflowRun struct {
    Spec   WorkflowRunSpec
    Status WorkflowRunStatus
}

type WorkflowRunSpec struct {
    WorkflowRef string        // Reference to Workflow template (mutually exclusive with Inline)
    Inline      *WorkflowSpec // Inline workflow definition (mutually exclusive with WorkflowRef)
}

type WorkflowRunStatus struct {
    Phase          WorkflowPhase           // Pending|Running|Completed|Failed
    CurrentStage   int32                   // Index of current stage (-1 = not started)
    TotalTasks     int32                   // Total tasks across all stages
    CompletedTasks int32                   // Number of completed tasks
    FailedTasks    int32                   // Number of failed tasks
    StartTime      *metav1.Time            // When workflow run started
    CompletionTime *metav1.Time            // When workflow run finished
    StageStatuses  []WorkflowStageStatus   // Status of each stage
    Conditions     []metav1.Condition
}

type WorkflowStageStatus struct {
    Name           string        // Stage name
    Phase          WorkflowPhase // Stage phase
    Tasks          []string      // Actual Task CR names created
    StartTime      *metav1.Time
    CompletionTime *metav1.Time
}

type WorkflowPhase string
const (
    WorkflowPhasePending   WorkflowPhase = "Pending"
    WorkflowPhaseRunning   WorkflowPhase = "Running"
    WorkflowPhaseCompleted WorkflowPhase = "Completed"
    WorkflowPhaseFailed    WorkflowPhase = "Failed"
)

// CronWorkflow represents scheduled WorkflowRun triggering
type CronWorkflow struct {
    Spec   CronWorkflowSpec
    Status CronWorkflowStatus
}

type CronWorkflowSpec struct {
    Schedule    string        // Cron expression (e.g., "0 9 * * *")
    Suspend     *bool         // Suspend scheduling
    WorkflowRef string        // Reference to Workflow template (mutually exclusive with Inline)
    Inline      *WorkflowSpec // Inline workflow definition (mutually exclusive with WorkflowRef)
}

type CronWorkflowStatus struct {
    Active             []corev1.ObjectReference // Currently running WorkflowRuns
    LastScheduleTime   *metav1.Time             // Last scheduled time
    LastSuccessfulTime *metav1.Time             // Last successful completion
    Conditions         []metav1.Condition
}

// WebhookTrigger represents a webhook-to-Task mapping rule
type WebhookTrigger struct {
    Spec   WebhookTriggerSpec
    Status WebhookTriggerStatus
}

type WebhookTriggerSpec struct {
    Auth              *WebhookAuth             // Authentication configuration
    MatchPolicy       MatchPolicy              // First (default) or All
    Rules             []WebhookRule            // Multiple filter-to-resourceTemplate mappings
    Filter            string                   // DEPRECATED: CEL expression for filtering
    ConcurrencyPolicy ConcurrencyPolicy        // Allow, Forbid, or Replace
    TaskTemplate      *WebhookTaskTemplate     // DEPRECATED: use ResourceTemplate
    ResourceTemplate  *WebhookResourceTemplate // Task, WorkflowRun, or WorkflowRef
}

// WebhookRule defines a single rule for rules-based triggers
type WebhookRule struct {
    Name              string                  // Unique rule identifier
    Filter            string                  // CEL expression for filtering
    ConcurrencyPolicy ConcurrencyPolicy       // Per-rule override
    ResourceTemplate  WebhookResourceTemplate // What to create when matched
}

// WebhookResourceTemplate defines Task or WorkflowRun to create
type WebhookResourceTemplate struct {
    Task        *WebhookTaskSpec        // Create Task
    WorkflowRef string                  // Reference Workflow, create WorkflowRun
    WorkflowRun *WebhookWorkflowRunSpec // Inline WorkflowRun
}

type WebhookAuth struct {
    HMAC        *HMACAuth        // HMAC signature validation
    BearerToken *BearerTokenAuth // Bearer token validation
    Header      *HeaderAuth      // Custom header validation
}

type HMACAuth struct {
    SecretRef       SecretKeyReference // Secret containing HMAC key
    SignatureHeader string             // Header containing signature (e.g., "X-Hub-Signature-256")
    Algorithm       string             // sha1, sha256, or sha512 (default: sha256)
}

type WebhookTaskTemplate struct {
    AgentRef    string          // Reference to Agent (optional)
    Description string          // Go template with webhook payload data
    Contexts    []ContextSource // Additional contexts
}

type ConcurrencyPolicy string
const (
    ConcurrencyPolicyAllow   ConcurrencyPolicy = "Allow"   // Create new Task regardless of existing
    ConcurrencyPolicyForbid  ConcurrencyPolicy = "Forbid"  // Skip if Task already running
    ConcurrencyPolicyReplace ConcurrencyPolicy = "Replace" // Stop existing, create new
)

type WebhookTriggerStatus struct {
    LastTriggeredTime *metav1.Time         // When last triggered
    TotalTriggered    int64                // Total trigger count
    ActiveTasks       []string             // DEPRECATED: use ActiveResources
    ActiveResources   []string             // Currently running Task/WorkflowRun names
    RuleStatuses      []WebhookRuleStatus  // Per-rule status (only for rules-based)
    WebhookURL        string               // Full webhook URL path
    Conditions        []metav1.Condition
}

type WebhookRuleStatus struct {
    Name              string       // Rule name
    LastTriggeredTime *metav1.Time // When this rule was last triggered
    TotalTriggered    int64        // Times this rule triggered
    ActiveResources   []string     // Active resources for this rule
}

// Context represents a reusable context resource
type Context struct {
    Spec ContextSpec
}

type ContextSpec struct {
    Type      ContextType       // Text, ConfigMap, Git, or Runtime
    Text      string            // Text content (when Type is Text)
    ConfigMap *ConfigMapContext // Reference to ConfigMap
    Git       *GitContext       // Content from Git repository
    Runtime   *RuntimeContext   // Platform awareness context
}

type ContextType string
const (
    ContextTypeText      ContextType = "Text"
    ContextTypeConfigMap ContextType = "ConfigMap"
    ContextTypeGit       ContextType = "Git"
    ContextTypeRuntime   ContextType = "Runtime"
)

// RuntimeContext enables KubeTask platform awareness for agents.
// When enabled, the controller injects a system prompt explaining:
// - The agent is running in a Kubernetes environment as a KubeTask Task
// - Available environment variables (TASK_NAME, TASK_NAMESPACE, WORKSPACE_DIR)
// - How to query Task/Workflow information via kubectl
type RuntimeContext struct {
    // No fields - content is generated by the controller
}

type ConfigMapContext struct {
    Name     string // Name of the ConfigMap
    Key      string // Optional: specific key to mount
    Optional *bool  // Whether the ConfigMap must exist
}

type GitContext struct {
    Repository string              // Git repository URL
    Path       string              // Path within the repository
    Ref        string              // Branch, tag, or commit SHA (default: "HEAD")
    Depth      *int                // Shallow clone depth (default: 1)
    SecretRef  *GitSecretReference // Optional Git credentials
}

// Agent defines the AI agent configuration
type Agent struct {
    Spec AgentSpec
}

type AgentSpec struct {
    AgentImage         string
    WorkspaceDir       string           // Working directory (default: "/workspace")
    Command            []string         // Custom entrypoint command
    Contexts           []ContextSource  // Reference or inline contexts
    Credentials        []Credential
    PodSpec            *AgentPodSpec    // Pod configuration (labels, scheduling, runtime)
    ServiceAccountName string
    HumanInTheLoop     *HumanInTheLoop  // Enable session sidecar for debugging
}

// HumanInTheLoop configures session strategies for human interaction
type HumanInTheLoop struct {
    // Shared configuration for both session strategies
    Image   string          // Custom image for session containers (default: agentImage)
    Command []string        // Custom command for session containers (overrides sidecar.duration)
    Ports   []ContainerPort // Ports to expose for port-forwarding

    // Strategy-specific configuration
    Sidecar     *SessionSidecar     // Ephemeral session sidecar
    Persistence *SessionPersistence // Durable workspace persistence
}

// SessionSidecar configures ephemeral session sidecar
type SessionSidecar struct {
    Enabled  bool             // Enable session sidecar
    Duration *metav1.Duration // How long sidecar runs (default: "1h", ignored if command is set)
}

// SessionPersistence configures durable workspace persistence
type SessionPersistence struct {
    Enabled bool // Enable workspace persistence to PVC
}

// ContainerPort defines a port to expose on the sidecar container
type ContainerPort struct {
    Name          string          // Optional name for this port
    ContainerPort int32           // Port number to expose (1-65535)
    Protocol      corev1.Protocol // TCP (default) or UDP
}

// KubeTaskConfig defines system-level configuration
type KubeTaskConfig struct {
    Spec KubeTaskConfigSpec
}

type KubeTaskConfigSpec struct {
    TaskLifecycle *TaskLifecycleConfig
    SessionPVC    *SessionPVCConfig    // PVC infrastructure for session persistence
}

type TaskLifecycleConfig struct {
    TTLSecondsAfterFinished *int32  // TTL for completed/failed tasks (default: 604800 = 7 days)
}

// SessionPVCConfig provides PVC infrastructure for session persistence.
// Whether persistence is enabled is controlled per-Agent via humanInTheLoop.persistence.enabled.
type SessionPVCConfig struct {
    Name             string                  // PVC name (default: "kubetask-session-data")
    StorageClassName *string                 // StorageClass for PVC
    StorageSize      string                  // PVC size (default: "10Gi")
    RetentionPolicy  *SessionRetentionPolicy // Cleanup policy
}

type SessionRetentionPolicy struct {
    TTLSecondsAfterTaskDeletion *int32 // How long to keep session data (default: 604800 = 7 days)
}
```

---

## System Architecture

### Component Layers

```
┌─────────────────────────────────────────────────────────────┐
│                   Kubernetes API Server                     │
│  - Custom Resource Definitions (CRDs)                       │
│  - RBAC & Authentication                                    │
│  - Event System                                             │
└─────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│              KubeTask Controller (Operator)                 │
│  - Watch Task CRs                                           │
│  - Reconcile loop                                           │
│  - Create Kubernetes Jobs for tasks                         │
│  - Update CR status fields                                  │
│  - Handle retries and failures                              │
└─────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                   Kubernetes Jobs/Pods                      │
│  - Each task runs as a separate Job/Pod                     │
│  - Execute task using agent container                       │
│  - AI agent invocation                                      │
│  - Context files mounted as volumes                         │
└─────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                      Storage Layer                          │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ etcd (Kubernetes Backend)                            │   │
│  │  - Task CRs                                          │   │
│  │  - Agent CRs                                         │   │
│  │  - CR status (execution state, results)              │   │
│  └──────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ ConfigMaps                                           │   │
│  │  - Task context files                                │   │
│  │  - Configuration data                                │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

---

## Custom Resource Definitions

### Task (Primary API)

Task is the primary API for executing AI-powered tasks.

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: update-service-a
  namespace: kubetask-system
spec:
  # Simple task description (syntactic sugar for /workspace/task.md)
  description: |
    Update dependencies to latest versions.
    Run tests and create PR.

  # Reference reusable Context CRDs
  contexts:
    - name: coding-standards
      mountPath: /workspace/guides/standards.md
    - name: security-policy
      # Empty mountPath = append to task.md with XML tags

  # Optional: Reference to Agent (defaults to "default")
  agentRef: my-agent

status:
  # Execution phase
  phase: Running  # Pending|Running|Completed|Failed

  # Kubernetes Job name
  jobName: update-service-a-xyz123

  # Start and end times
  startTime: "2025-01-18T10:00:00Z"
  completionTime: "2025-01-18T10:05:00Z"
```

**Field Description:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.description` | String | No | Task instruction (creates /workspace/task.md) |
| `spec.contexts` | []ContextSource | No | Reference or inline contexts (see below) |
| `spec.agentRef` | String | No | Reference to Agent (default: "default") |

**Status Field Description:**

| Field | Type | Description |
|-------|------|-------------|
| `status.phase` | TaskPhase | Execution phase: Pending\|Queued\|Running\|Completed\|Failed |
| `status.jobName` | String | Kubernetes Job name |
| `status.startTime` | Timestamp | Start time |
| `status.completionTime` | Timestamp | End time |

**ContextSource Types:**

Contexts can be defined in two ways:

1. **Reference** (`ref`) - Reference an existing Context CRD:
```yaml
contexts:
  - ref:
      name: coding-standards
      mountPath: /workspace/guides/standards.md  # Optional
```

2. **Inline** (`inline`) - Define context directly in Task/Agent:
```yaml
contexts:
  - inline:
      type: Git
      mountPath: ${WORKSPACE_DIR}
      git:
        repository: https://github.com/org/repo
        ref: main
```

**Context CRD Types:**

Contexts are defined using the Context CRD:

1. **Inline Context**:
```yaml
apiVersion: kubetask.io/v1alpha1
kind: Context
metadata:
  name: coding-standards
spec:
  type: Text
  text: "Task description or guidelines"
```

2. **ConfigMap Context**:
```yaml
apiVersion: kubetask.io/v1alpha1
kind: Context
metadata:
  name: org-config
spec:
  type: ConfigMap
  configMap:
    name: my-configs
    key: config.md  # Optional: specific key
```

3. **Git Context**:
```yaml
apiVersion: kubetask.io/v1alpha1
kind: Context
metadata:
  name: repo-context
spec:
  type: Git
  git:
    repository: https://github.com/org/contexts
    path: .claude/
    ref: main
```

### Workflow (Reusable Template)

Workflow is a template resource that defines a multi-stage task structure. It does NOT execute - to run a workflow, create a WorkflowRun that references it.

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Workflow
metadata:
  name: ci-pipeline
  namespace: kubetask-system
spec:
  stages:
    # Stage 0: Lint (name auto-generated as "stage-0")
    - tasks:
        - name: lint
          spec:
            description: "Run linting checks"
            agentRef: claude

    # Stage 1: Testing (explicit name)
    - name: testing
      tasks:
        - name: test-unit
          spec:
            description: "Run unit tests"
            agentRef: claude
        - name: test-e2e
          spec:
            description: "Run e2e tests"
            agentRef: gemini

    # Stage 2: Deploy (name auto-generated as "stage-2")
    - tasks:
        - name: deploy
          spec:
            description: "Deploy to staging"
            agentRef: claude
# Note: No status field - Workflow is a template only
```

**Field Description:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.stages` | []WorkflowStage | Yes | Sequential stages of the workflow |
| `spec.stages[].name` | String | No | Stage name (auto-generated as "stage-N" if not specified) |
| `spec.stages[].tasks` | []WorkflowTask | Yes | Tasks to run in parallel within this stage |
| `spec.stages[].tasks[].name` | String | Yes | Unique task name within workflow |
| `spec.stages[].tasks[].spec` | TaskSpec | Yes | TaskSpec for the created Task |

### WorkflowRun (Execution Instance)

WorkflowRun executes a workflow, either by referencing a Workflow template or with an inline definition.

```
workflowrun = [[task] -> [task, task, task] -> [task]]
```

This example has 3 stages:
- Stage 0: 1 task
- Stage 1: 3 tasks in parallel (starts after stage 0 completes)
- Stage 2: 1 task (starts after all stage 1 tasks complete)

**WorkflowRun with workflowRef:**

```yaml
apiVersion: kubetask.io/v1alpha1
kind: WorkflowRun
metadata:
  name: ci-pipeline-run-001
  namespace: kubetask-system
spec:
  workflowRef: ci-pipeline  # Reference to Workflow template
status:
  phase: Running
  currentStage: 1
  totalTasks: 4
  completedTasks: 1
  failedTasks: 0
  startTime: "2025-01-18T10:00:00Z"
  stageStatuses:
    - name: stage-0
      phase: Completed
      tasks: ["ci-pipeline-run-001-lint"]
      startTime: "2025-01-18T10:00:00Z"
      completionTime: "2025-01-18T10:02:00Z"
    - name: testing
      phase: Running
      tasks: ["ci-pipeline-run-001-test-unit", "ci-pipeline-run-001-test-e2e"]
      startTime: "2025-01-18T10:02:00Z"
    - name: stage-2
      phase: Pending
      tasks: []
```

**WorkflowRun with inline definition:**

```yaml
apiVersion: kubetask.io/v1alpha1
kind: WorkflowRun
metadata:
  name: adhoc-run
  namespace: kubetask-system
spec:
  inline:
    stages:
      - tasks:
          - name: quick-task
            spec:
              description: "One-off task"
              agentRef: claude
```

**Field Description:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.workflowRef` | String | Either this or inline | Reference to Workflow template |
| `spec.inline` | WorkflowSpec | Either this or workflowRef | Inline workflow definition |

**Status Field Description:**

| Field | Type | Description |
|-------|------|-------------|
| `status.phase` | WorkflowPhase | Workflow phase: Pending\|Running\|Completed\|Failed |
| `status.currentStage` | int32 | Index of currently executing stage (-1 = not started) |
| `status.totalTasks` | int32 | Total number of tasks across all stages |
| `status.completedTasks` | int32 | Number of completed tasks |
| `status.failedTasks` | int32 | Number of failed tasks |
| `status.stageStatuses` | []WorkflowStageStatus | Status of each stage |

**Task Naming:**

Created Tasks are named `{workflowrun-name}-{task-name}` (e.g., `ci-pipeline-run-001-lint`).

**Dependency Tracking:**

Tasks created by WorkflowRun have labels and annotations for dependency tracking:

```yaml
# Labels on created Tasks
labels:
  kubetask.io/workflow-run: ci-pipeline-run-001
  kubetask.io/workflow: ci-pipeline  # Only if workflowRef was used
  kubetask.io/stage: testing
  kubetask.io/stage-index: "1"

# Annotation for dependencies (comma-separated Task CR names)
annotations:
  kubetask.io/depends-on: "ci-pipeline-run-001-lint"
```

**Failure Handling:**

WorkflowRun uses a **Fail Fast** strategy:
- If any task fails, the workflow run immediately enters `Failed` phase
- No further stages are started
- Tasks already running in the current stage continue to completion

**Key Behaviors:**

1. **Stage Progression**: Stage N+1 starts only after ALL tasks in stage N complete successfully
2. **Parallel Execution**: All tasks within a stage start simultaneously
3. **Garbage Collection**: Tasks have OwnerReference to WorkflowRun for cascade deletion
4. **No Data Passing**: Tasks don't pass data between stages (AI outputs are unstructured)

### CronWorkflow (Scheduled Execution)

CronWorkflow creates WorkflowRun resources on a schedule, similar to how Kubernetes CronJob creates Jobs.

```yaml
apiVersion: kubetask.io/v1alpha1
kind: CronWorkflow
metadata:
  name: daily-ci
  namespace: kubetask-system
spec:
  # Cron schedule (required)
  schedule: "0 9 * * *"  # Every day at 9:00 AM

  # Reference to Workflow template (mutually exclusive with inline)
  workflowRef: ci-pipeline

  # Or inline definition:
  # inline:
  #   stages:
  #     - tasks:
  #         - name: daily-task
  #           spec:
  #             description: "Daily CI task"
  #             agentRef: claude

  # Suspend scheduling (optional, default: false)
  suspend: false

status:
  # Currently running WorkflowRuns
  active:
    - name: daily-ci-1737190800
      namespace: kubetask-system

  # Last scheduled time
  lastScheduleTime: "2025-01-18T09:00:00Z"

  # Last successful completion
  lastSuccessfulTime: "2025-01-17T09:05:00Z"

  # Conditions
  conditions:
    - type: Scheduled
      status: "True"
      reason: WorkflowRunCreated
      message: "Created WorkflowRun daily-ci-1737190800"
```

**Field Description:**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `spec.schedule` | String | Yes | - | Cron expression (e.g., "0 9 * * *") |
| `spec.workflowRef` | String | Either this or inline | - | Reference to Workflow template |
| `spec.inline` | WorkflowSpec | Either this or workflowRef | - | Inline workflow definition |
| `spec.suspend` | Bool | No | false | Suspend scheduling |

**WorkflowRun Naming:**

Created WorkflowRuns are named `{cronworkflow-name}-{unix-timestamp}` (e.g., `daily-ci-1737190800`).

**Concurrency Policy:**

CronWorkflow uses a **Forbid** policy - if a WorkflowRun is still active when the next schedule triggers, the new run is skipped.

### WebhookTrigger (Event-Driven Triggering)

WebhookTrigger enables event-driven Task or WorkflowRun creation from external webhooks (GitHub, GitLab, custom systems, etc.). It supports two modes:

1. **Legacy mode**: Single filter + taskTemplate (for backward compatibility)
2. **Rules-based mode**: Multiple rules with different filters triggering different resources

**Multi-Rule Example (Recommended):**

```yaml
apiVersion: kubetask.io/v1alpha1
kind: WebhookTrigger
metadata:
  name: github-events
  namespace: kubetask-system
spec:
  auth:
    hmac:
      secretRef:
        name: github-webhook-secret
        key: secret
      signatureHeader: X-Hub-Signature-256

  matchPolicy: First  # First matching rule triggers (or All for multiple)
  concurrencyPolicy: Allow  # Default for all rules

  rules:
    # Rule 1: PR review -> Task
    - name: pr-review
      filter: |
        headers["x-github-event"] == "pull_request" &&
        body.action in ["opened", "synchronize"]
      resourceTemplate:
        task:
          agentRef: code-review-agent
          description: |
            Review PR #{{ .pull_request.number }}
            Repository: {{ .repository.full_name }}

    # Rule 2: Complex issue -> Workflow
    - name: issue-workflow
      filter: |
        headers["x-github-event"] == "issues" &&
        body.action == "opened" &&
        body.issue.labels.exists(l, l.name == "needs-analysis")
      resourceTemplate:
        workflowRef: issue-analysis-workflow

    # Rule 3: Simple issue -> Task
    - name: issue-triage
      filter: |
        headers["x-github-event"] == "issues" &&
        body.action == "opened"
      concurrencyPolicy: Forbid  # Per-rule override
      resourceTemplate:
        task:
          agentRef: triage-agent
          description: |
            Triage Issue #{{ .issue.number }}

status:
  webhookURL: /webhooks/kubetask-system/github-events
  lastTriggeredTime: "2025-01-18T10:00:00Z"
  totalTriggered: 42
  activeResources:
    - github-events-pr-review-abc123
  ruleStatuses:
    - name: pr-review
      lastTriggeredTime: "2025-01-18T10:00:00Z"
      totalTriggered: 30
      activeResources:
        - github-events-pr-review-abc123
    - name: issue-workflow
      totalTriggered: 5
    - name: issue-triage
      totalTriggered: 7
```

**Legacy Example (for backward compatibility):**

```yaml
apiVersion: kubetask.io/v1alpha1
kind: WebhookTrigger
metadata:
  name: github-pr-review
spec:
  auth:
    hmac:
      secretRef:
        name: github-webhook-secret
        key: secret
      signatureHeader: X-Hub-Signature-256
  filter: 'body.action in ["opened", "synchronize"]'
  concurrencyPolicy: Replace
  taskTemplate:
    agentRef: code-review-agent
    description: |
      Review PR #{{ .pull_request.number }}
```

**Field Description:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.auth` | WebhookAuth | No | Authentication configuration (HMAC, Bearer, or Header) |
| `spec.matchPolicy` | String | No | First (default) or All for rules-based mode |
| `spec.rules` | []WebhookRule | No | Multiple filter-to-resourceTemplate mappings |
| `spec.rules[].resourceTemplate.task` | WebhookTaskSpec | - | Create a Task |
| `spec.rules[].resourceTemplate.workflowRef` | String | - | Reference Workflow, create WorkflowRun |
| `spec.rules[].resourceTemplate.workflowRun` | WebhookWorkflowRunSpec | - | Inline WorkflowRun |
| `spec.filter` | String | No | DEPRECATED: CEL expression (use rules[].filter) |
| `spec.taskTemplate` | WebhookTaskTemplate | No | DEPRECATED: use resourceTemplate |

**Resource Naming:**

Created resources are named `{trigger-name}-{rule-name}-{random}` (e.g., `github-events-pr-review-abc123`).

**Authentication Methods:**

| Method | Use Case | Example |
|--------|----------|---------|
| `auth.hmac` | GitHub, HMAC-based systems | `X-Hub-Signature-256` header |
| `auth.bearerToken` | OAuth systems | `Authorization: Bearer <token>` |
| `auth.header` | GitLab, custom systems | `X-Gitlab-Token` header |

**CEL Filter Variables:**

| Variable | Type | Description |
|----------|------|-------------|
| `body` | dynamic | Webhook JSON payload |
| `headers` | map[string]string | HTTP headers (lowercase keys) |

**CEL Filter Examples:**

```yaml
# Simple equality
filter: 'body.action == "opened"'

# Multiple conditions
filter: 'body.action in ["opened", "synchronize"] && body.repository.full_name == "myorg/myrepo"'

# String functions
filter: '!body.pull_request.title.startsWith("[WIP]")'

# Existence checks
filter: 'has(body.pull_request) && body.pull_request.draft == false'

# List predicates
filter: 'body.pull_request.labels.exists(l, l.name == "needs-review")'

# Header-based filtering
filter: 'headers["x-github-event"] == "pull_request"'
```

**Concurrency Policies:**

| Policy | Behavior |
|--------|----------|
| `Allow` | Create new Task regardless of existing running Tasks (default) |
| `Forbid` | Skip webhook if there's already a running Task |
| `Replace` | Stop running Task(s) and create new one |

**Webhook URL:**

Each WebhookTrigger has a unique endpoint at `/webhooks/<namespace>/<trigger-name>`. Configure this URL in your webhook source (GitHub, GitLab, etc.).

### Context (Reusable Context)

Context represents a reusable context resource for AI agent tasks. Context CRDs enable:
- **Reusability**: Share the same context across multiple Tasks
- **Independent lifecycle**: Update context without modifying Tasks
- **Version control**: Track context changes in Git
- **Separation of concerns**: Context content vs. mount location

Context supports three source types:
- **Inline**: Content directly in YAML
- **ConfigMap**: Reference to a ConfigMap (key or entire ConfigMap)
- **Git**: Content from a Git repository (future)

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Context
metadata:
  name: coding-standards
  namespace: kubetask-system
spec:
  # Type of context: Inline, ConfigMap, or Git
  type: Inline

  # Inline content
  inline:
    content: |
      # Coding Standards
      - Use descriptive variable names
      - Write unit tests for all functions
      - Follow Go conventions
```

**Context from ConfigMap:**

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Context
metadata:
  name: security-policy
spec:
  type: ConfigMap
  configMap:
    name: org-policies
    key: security.md
```

**Field Description:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.type` | ContextType | Yes | Type of context: Text, ConfigMap, Git, or Runtime |
| `spec.text` | string | When type=Text | Text content |
| `spec.configMap` | ConfigMapContext | When type=ConfigMap | Reference to ConfigMap |
| `spec.git` | GitContext | When type=Git | Content from Git repository |
| `spec.runtime` | RuntimeContext | When type=Runtime | Platform awareness (no fields - content is generated by controller) |

**Important Notes:**

- **No mount path in Context CRD**: The mount path is defined by the referencing Task/Agent via `ContextRef.mountPath` or `ContextItem.mountPath`
- **No Status**: Context is a pure data resource (like ConfigMap) with no controller reconciliation
- **Empty MountPath behavior**: When mountPath is empty, content is appended to `/workspace/task.md` with XML tags
- **Runtime context**: Provides KubeTask platform awareness to agents, explaining environment variables, kubectl commands, and system concepts

**Context Priority (lowest to highest):**

1. Agent.contexts (array order)
2. Task.contexts (array order)
3. Task.description (becomes start of /workspace/task.md)

### Agent (Execution Configuration)

Agent defines the AI agent configuration for task execution.

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: default  # Convention: "default" is used when no agentRef is specified
  namespace: kubetask-system
spec:
  # Agent container image
  agentImage: quay.io/kubetask/kubetask-agent-gemini:latest

  # Optional: Working directory (default: "/workspace")
  workspaceDir: /workspace

  # Optional: Custom entrypoint command (required when Task has humanInTheLoop enabled)
  command: ["sh", "-c", "gemini --yolo -p \"$(cat /workspace/task.md)\""]

  # Optional: Reference reusable Context CRDs (applied to all tasks using this agent)
  contexts:
    - name: org-coding-standards
      # Empty mountPath = append to task.md with XML tags
    - name: org-security-policy

  # Optional: Credentials (secrets as env vars or file mounts)
  credentials:
    # Mount entire secret as environment variables (all keys become env vars)
    - name: api-keys
      secretRef:
        name: api-credentials
        # No key specified - all secret keys become ENV vars with same names

    # Mount single key with custom env name
    - name: github-token
      secretRef:
        name: github-creds
        key: token
      env: GITHUB_TOKEN

    # Mount single key as file
    - name: ssh-key
      secretRef:
        name: ssh-keys
        key: id_rsa
      mountPath: /home/agent/.ssh/id_rsa
      fileMode: 0400

  # Optional: Advanced Pod configuration
  podSpec:
    # Labels for NetworkPolicy, monitoring, etc.
    labels:
      network-policy: agent-restricted

    # Scheduling constraints
    scheduling:
      nodeSelector:
        kubernetes.io/os: linux
      tolerations:
        - key: "dedicated"
          operator: "Equal"
          value: "ai-workload"
          effect: "NoSchedule"

    # RuntimeClass for enhanced isolation (gVisor, Kata, etc.)
    runtimeClassName: gvisor

  # Required: ServiceAccount for agent pods
  serviceAccountName: kubetask-agent
```

**Field Description:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.agentImage` | String | No | Agent container image |
| `spec.workspaceDir` | String | No | Working directory (default: "/workspace") |
| `spec.command` | []String | No | Custom entrypoint command |
| `spec.contexts` | []ContextSource | No | Reference or inline contexts (applied to all tasks) |
| `spec.credentials` | []Credential | No | Secrets as env vars or file mounts |
| `spec.podSpec` | *AgentPodSpec | No | Advanced Pod configuration (labels, scheduling, runtimeClass) |
| `spec.serviceAccountName` | String | Yes | ServiceAccount for agent pods |

**PodSpec Configuration:**

The `podSpec` field groups all Pod-level settings:

| Field | Type | Description |
|-------|------|-------------|
| `podSpec.labels` | map[string]string | Additional labels for the pod (for NetworkPolicy, monitoring) |
| `podSpec.scheduling` | *PodScheduling | Node selector, tolerations, affinity |
| `podSpec.runtimeClassName` | String | RuntimeClass for container isolation (gVisor, Kata) |

**RuntimeClass for Enhanced Isolation:**

When running untrusted AI agent code, you can use `runtimeClassName` to specify a more secure container runtime:

```yaml
podSpec:
  runtimeClassName: gvisor  # or "kata" for Kata Containers
```

This provides an additional layer of security beyond standard container isolation. The RuntimeClass must exist in the cluster before use. See [Kubernetes RuntimeClass documentation](https://kubernetes.io/docs/concepts/containers/runtime-class/) for details.

**Human-in-the-Loop:**

KubeTask supports two complementary session strategies for human interaction:

1. **Session Sidecar (Ephemeral)**: A sidecar container that runs alongside the agent, providing **immediate but temporary** access after task completion.

2. **Session Persistence (Durable)**: Workspace content is saved to a PVC, enabling **resume from a new Pod** after the original Pod terminates.

Both strategies can be enabled independently or together via `humanInTheLoop`:

```yaml
# Agent with humanInTheLoop settings
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: dev-agent
spec:
  agentImage: quay.io/kubetask/kubetask-agent-gemini:latest
  command: ["sh", "-c", "gemini -p \"$(cat /workspace/task.md)\""]
  workspaceDir: /workspace
  serviceAccountName: kubetask-agent
  humanInTheLoop:
    # Shared configuration (used by both strategies)
    image: ""  # Optional: defaults to agentImage
    ports:
      - name: dev-server
        containerPort: 3000

    # Session Sidecar (ephemeral)
    sidecar:
      enabled: true
      duration: "2h"

    # Session Persistence (durable)
    persistence:
      enabled: true  # Requires sessionPVC configured in KubeTaskConfig
```

**Task Annotation:**

When a Task is created with an Agent that has either `humanInTheLoop.sidecar.enabled: true` or `humanInTheLoop.persistence.enabled: true`, the controller automatically adds the following annotation to the Task:

```yaml
annotations:
  kubetask.io/human-in-the-loop: "true"
```

This annotation allows users and tools to easily identify Tasks that have human-in-the-loop enabled, without needing to look up the Agent configuration.

**Sidecar Container:**

When `sidecar.enabled` is true, the Pod will have two containers:
1. `agent` - Executes the task with the original command (exits when done)
2. `session` - Runs `sleep` for the specified duration, allowing user access

Users can exec into the sidecar container to use the same tools as the agent:

```bash
# Access the session sidecar container
kubectl exec -it <pod-name> -c session -- /bin/bash

# Inside the container, you can use the same tools
$ ls /workspace          # View agent's output files
$ gemini --yolo "..."    # Use the same gemini CLI
```

**Custom Sidecar Image:**

By default, the sidecar uses the same image as the Agent. You can specify a custom image:

```yaml
humanInTheLoop:
  image: "busybox:stable"  # Lightweight image to save resources
  sidecar:
    enabled: true
```

**Custom Sidecar Command:**

For advanced scenarios like running code-server or other services, use the `command` field instead of `sidecar.duration`:

```yaml
humanInTheLoop:
  image: quay.io/kubetask/kubetask-agent-code-server:latest
  command:
    - sh
    - -c
    - code-server --bind-addr 0.0.0.0:8080 ${WORKSPACE_DIR} & sleep 7200
  ports:
    - name: code-server
      containerPort: 8080
  sidecar:
    enabled: true
    # duration is ignored when command is specified
```

Note: When `command` is specified, `sidecar.duration` is ignored.

**Port Forwarding:**

For development tasks that need to expose network services (e.g., dev servers, APIs), configure ports in the `humanInTheLoop` section:

```yaml
spec:
  humanInTheLoop:
    ports:
      - name: dev-server
        containerPort: 3000
      - name: api
        containerPort: 8080
        protocol: TCP  # TCP (default) or UDP
    sidecar:
      enabled: true
      duration: "2h"
```

Access ports via:

```bash
kubectl port-forward pod/<pod-name> 3000:3000 8080:8080
```

**Early Stop:**

To exit a human-in-the-loop Task early (without waiting for the session duration timeout), set the stop annotation:

```bash
kubectl annotate task my-task kubetask.io/stop=true
```

This stops the Task gracefully and sets its status to `Completed` with a `Stopped` condition.

**How Task Stop Works (Technical Details):**

When the stop annotation is detected, the controller leverages Kubernetes native Job suspension:

1. **Controller sets `job.spec.suspend = true`** on the associated Job
2. **Kubernetes sends SIGTERM** to all running Pods (including the agent and any sidecars like code-server)
3. **Graceful termination period** (default 30 seconds) is honored, allowing processes to clean up
4. **Pod transitions to `Failed` state** - the Pod is NOT deleted, so logs remain accessible
5. **Task status is set to `Completed`** with a `Stopped` condition (reason: `UserStopped`)

This approach ensures:
- **Logs are preserved**: Unlike deleting the Job/Pod, suspending keeps all resources intact
- **Graceful shutdown**: Containers receive SIGTERM and can clean up properly
- **Sidecars are terminated**: All containers (agent + sidecars) receive the signal
- **Resources remain for inspection**: Job and Pod exist until TTL-based cleanup (default 7 days)

Reference: [Kubernetes Jobs - Suspending a Job](https://kubernetes.io/docs/concepts/workloads/controllers/job/#suspending-a-job)

**Session Persistence:**

When `humanInTheLoop.persistence.enabled` is true on the Agent and `sessionPVC` is configured in KubeTaskConfig, workspace content is automatically saved to a shared PVC after Task completion. This enables users to resume work later via a session Pod.

**How Session Persistence Works:**

1. A `save-session` sidecar is added to the Task Pod
2. After the agent container completes, save-session copies workspace to PVC
3. Workspace is saved at `/<namespace>/<task-name>/` on the PVC
4. User can trigger resume by adding annotation: `kubetask.io/resume-session=true`
5. Controller creates a session Pod with the same image, credentials, and environment

**Comparison of Session Strategies:**

| Aspect | Session Sidecar | Session Persistence |
|--------|-----------------|---------------------|
| Availability | Immediate | After annotation trigger |
| Duration | Fixed (e.g., 1h) | Unlimited (within retention) |
| Workspace after timeout | Lost | Preserved |
| Resource when idle | Pod running | Only PVC storage |
| Resume capability | No | Yes |
| Setup complexity | Low | Medium (requires RWX PVC) |

**Combined Usage:**

When both strategies are enabled, users get the best of both:
- Session sidecar provides immediate access after Task completion
- Workspace is saved to PVC in parallel
- If sidecar times out, user can resume via annotation later

**Resume Session:**

```bash
# Trigger session resume
kubectl annotate task my-task kubetask.io/resume-session=true

# Check session status
kubectl get task my-task -o jsonpath='{.status.sessionStatus}'

# Access session Pod
kubectl exec -it my-task-session -c session -- /bin/bash

# Stop session when done
kubectl annotate task my-task kubetask.io/stop=true
```

**Session Pod Labels:**

Session Pods have specific labels for easy identification:

```yaml
labels:
  kubetask.io/task: <task-name>
  kubetask.io/session-task: <task-name>
  kubetask.io/component: session
```

**Session Cleanup:**

When a Task with session persistence is deleted, the controller automatically cleans up the persisted session data from the PVC.

**How Cleanup Works:**

1. When a Task with session persistence is created, a finalizer `kubetask.io/session-cleanup` is added
2. When the Task is deleted, the controller creates a cleanup Job before allowing deletion
3. The cleanup Job runs `rm -rf /<namespace>/<task-name>/` on the session PVC
4. After the cleanup Job completes (or fails after 3 retries), the finalizer is removed
5. The Task deletion proceeds normally

**Cleanup Job Details:**

- Job name: `<task-name>-cleanup`
- Labels: `kubetask.io/cleanup: session`, `kubetask.io/task: <task-name>`
- Auto-deleted after 5 minutes (TTL)
- BackoffLimit: 3 retries

**Error Handling:**

If cleanup fails after retries, the finalizer is removed anyway to avoid blocking Task deletion indefinitely. In this case, orphaned files may remain on the PVC and require manual cleanup:

```bash
# List session directories on PVC (if using a debug Pod)
ls /pvc/<namespace>/

# Manually clean up orphaned session data
rm -rf /pvc/<namespace>/<task-name>/
```

---

## Agent Configuration

### Agent Image Discovery

Controller determines the agent image in this priority order:

1. **Agent.spec.agentImage** (from referenced Agent)
2. **Built-in default** (fallback) - `quay.io/kubetask/kubetask-agent-gemini:latest`

### How It Works

The controller:
1. Looks up the Agent referenced by `agentRef` (defaults to "default")
2. Uses the `agentImage` from Agent if specified
3. Falls back to built-in default image if no Agent or agentImage found
4. Generates a Job with:
   - Labels for tracking (`kubetask.io/task`)
   - Environment variables (`TASK_NAME`, `TASK_NAMESPACE`)
   - Owner references for garbage collection
   - ServiceAccount from Agent spec

### Context Priority

When a Task references an Agent, contexts are merged with the following priority (lowest to highest):

1. **Agent.contexts** (array order, lowest priority)
2. **Task.contexts** (array order)
3. **Task.description** (highest priority, becomes start of /workspace/task.md)

**MountPath Path Resolution:**

Path resolution follows [Tekton Workspaces](https://tekton.dev/docs/pipelines/workspaces/) conventions:

- **Absolute paths** (starting with `/`) are used as-is: `/etc/config/app.conf` → mounts at `/etc/config/app.conf`
- **Relative paths** (NOT starting with `/`) are prefixed with the agent's workspaceDir: `guides/readme.md` → mounts at `${workspaceDir}/guides/readme.md`

Examples:
```yaml
contexts:
  - ref:
      name: coding-standards
      mountPath: /etc/custom-config  # Absolute path - used as-is
  - ref:
      name: dev-guide
      mountPath: guides/readme.md     # Relative path - becomes ${workspaceDir}/guides/readme.md
```

**MountPath Behavior Summary:**

The following table summarizes how different context types behave based on `mountPath` configuration:

| Context Source | Context Type | mountPath | Behavior |
|----------------|--------------|-----------|----------|
| **Context CRD** (via `ref`) | Inline | Empty | Append to task.md with XML tags |
| **Context CRD** (via `ref`) | Inline | Specified | Mount as file at path |
| **Context CRD** (via `ref`) | ConfigMap (with key) | Empty | Append to task.md with XML tags |
| **Context CRD** (via `ref`) | ConfigMap (with key) | Specified | Mount as file at path |
| **Context CRD** (via `ref`) | ConfigMap (no key) | Empty | Append all keys to task.md |
| **Context CRD** (via `ref`) | ConfigMap (no key) | Specified | Mount as directory at path |
| **Context CRD** (via `ref`) | Git | Empty | Mount at `${WORKSPACE_DIR}/git-<context-name>` |
| **Context CRD** (via `ref`) | Git | Specified | Mount at specified path |
| **Inline** | Inline | Empty | Append to task.md with XML tags |
| **Inline** | Inline | Specified | Mount as file at path |
| **Inline** | ConfigMap | Empty/Specified | Same as Context CRD ConfigMap |
| **Inline** | Git | Empty | **Error**: mountPath required |
| **Inline** | Git | Specified | Mount at specified path |

**Key Rules:**
1. **Inline Git requires mountPath** - Unlike Context CRDs which have a name for auto-generation
2. **No duplicate mountPaths** - Controller returns error if two contexts target the same path
3. **Relative paths** - Prefixed with `${WORKSPACE_DIR}` (e.g., `guides/readme.md` → `/workspace/guides/readme.md`)
4. **Absolute paths** - Used as-is (e.g., `/etc/config`)

**Empty MountPath Behavior:**

When `mountPath` is empty (in either `ContextRef` or `ContextItem`), the context content is appended to `/workspace/task.md` with XML tags:

```xml
<context name="coding-standards" namespace="default" type="Inline">
... content ...
</context>
```

This enables multiple contexts to be aggregated into a single file that the agent reads.

**Inline Git Context Validation:**

Unlike Context CRDs (which have a `name` for automatic path generation like `git-<name>`), inline Git contexts require an explicit `mountPath`. This prevents conflicts when multiple inline Git contexts are used:

```yaml
# ❌ Invalid - inline Git without mountPath
contexts:
  - inline:
      type: Git
      git:
        repository: https://github.com/org/repo
      # Error: inline Git context requires mountPath

# ✅ Valid - inline Git with explicit mountPath
contexts:
  - inline:
      type: Git
      git:
        repository: https://github.com/org/repo
      mountPath: /workspace/my-repo
```

**Mount Path Conflict Detection:**

The controller validates that no two contexts mount to the same path. This prevents silent overwrites:

```yaml
# ❌ Invalid - duplicate mount paths
contexts:
  - ref:
      name: context-a
      mountPath: /workspace/config.yaml
  - ref:
      name: context-b
      mountPath: /workspace/config.yaml  # Error: mount path conflict
```

**Context Priority (Agent vs Task):**

When both Agent and Task define contexts, Task contexts are processed after Agent contexts. If they target the same `mountPath`, a conflict error is returned. To override Agent defaults, use different paths or omit the mountPath (append to task.md).

### Concurrency Control

Agents can limit the number of concurrent Tasks to prevent overwhelming backend AI services with rate limits:

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: claude-agent
spec:
  agentImage: quay.io/kubetask/kubetask-agent-claude:latest
  serviceAccountName: kubetask-agent
  maxConcurrentTasks: 3  # Only 3 Tasks can run concurrently
```

**Behavior:**

| `maxConcurrentTasks` Value | Behavior |
|---------------------------|----------|
| `nil` (not set) | Unlimited - all Tasks run immediately |
| `0` | Unlimited - same as nil |
| `> 0` | Limited - Tasks queue when at capacity |

**Task Lifecycle with Queuing:**

```
Task Created
    │
    ├─── Agent has capacity ──► Phase: Running ──► Phase: Completed/Failed
    │
    └─── Agent at capacity ──► Phase: Queued
                                    │
                                    ▼ (requeue every 10s)
                               Check capacity
                                    │
                                    ├─── Capacity available ──► Phase: Running
                                    │
                                    └─── Still at capacity ──► Remain Queued
```

**Implementation Details:**

- Tasks are labeled with `kubetask.io/agent: <agent-name>` for efficient capacity tracking
- Queued Tasks have a `Queued` condition with reason `AgentAtCapacity`
- Tasks are processed in approximate FIFO order based on creation timestamp
- When a running Task completes, queued Tasks are checked every 10 seconds

---

## System Configuration

### KubeTaskConfig (System-level Configuration)

KubeTaskConfig provides cluster or namespace-level settings for task lifecycle management and session PVC infrastructure.

```yaml
apiVersion: kubetask.io/v1alpha1
kind: KubeTaskConfig
metadata:
  name: default
  namespace: kubetask-system
spec:
  taskLifecycle:
    # TTL for completed/failed tasks before automatic deletion
    # Default: 604800 (7 days)
    # Set to 0 to disable automatic cleanup
    ttlSecondsAfterFinished: 604800

  # Session PVC infrastructure for human-in-the-loop persistence
  # This only provides the PVC - whether persistence is enabled is
  # controlled per-Agent via humanInTheLoop.persistence.enabled
  sessionPVC:
    name: kubetask-session-data
    storageClassName: ""  # Empty = use cluster default
    storageSize: "50Gi"
    retentionPolicy:
      ttlSecondsAfterTaskDeletion: 604800  # 7 days
```

**Field Description:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.taskLifecycle.ttlSecondsAfterFinished` | int32 | No | TTL in seconds for completed/failed tasks (default: 604800 = 7 days) |
| `spec.sessionPVC.name` | string | No | Shared PVC name (default: "kubetask-session-data") |
| `spec.sessionPVC.storageClassName` | string | No | StorageClass for PVC (empty = cluster default) |
| `spec.sessionPVC.storageSize` | string | No | PVC size (default: "10Gi") |
| `spec.sessionPVC.retentionPolicy.ttlSecondsAfterTaskDeletion` | int32 | No | How long to keep session data after Task deletion (default: 604800 = 7 days) |

**Session Persistence Requirements:**

- Agent must have `humanInTheLoop.persistence.enabled = true`
- KubeTaskConfig must have `sessionPVC` configured
- PVC must support ReadWriteMany (RWX) access mode for concurrent session access
- Workspace is saved at `/<namespace>/<task-name>/` on the PVC

### TTL-based Cleanup

The controller automatically deletes completed or failed Tasks after the configured TTL:

1. Task enters `Completed` or `Failed` phase
2. Controller records `CompletionTime`
3. After TTL expires, controller deletes the Task CR
4. Associated Job and ConfigMap are deleted via OwnerReference cascade

**Configuration Lookup Order:**

1. `KubeTaskConfig/default` in the Task's namespace
2. Built-in default (604800 seconds = 7 days)

**Disabling Cleanup:**

Set `ttlSecondsAfterFinished: 0` to disable automatic cleanup:

```yaml
spec:
  taskLifecycle:
    ttlSecondsAfterFinished: 0  # Disable automatic cleanup
```

### Future Extensions (TODO)

- **Historical Archiving**: Archive Tasks to external storage (S3, GCS) before deletion (similar to Tekton Results)
- **Retention by Count**: Keep the last N successful/failed tasks

---

## Complete Examples

### 1. Simple Task Execution

```yaml
# Create Agent
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: default
  namespace: kubetask-system
spec:
  agentImage: quay.io/kubetask/kubetask-agent-gemini:latest
  workspaceDir: /workspace
  serviceAccountName: kubetask-agent
---
# Create Task
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: update-service-a
  namespace: kubetask-system
spec:
  description: |
    Update dependencies to latest versions.
    Run tests and create PR.
```

### 2. Task with Multiple Context Sources

```yaml
# First, create reusable Context CRDs
apiVersion: kubetask.io/v1alpha1
kind: Context
metadata:
  name: refactoring-guide
  namespace: kubetask-system
spec:
  type: ConfigMap
  configMap:
    name: guides
    key: refactoring-guide.md
---
apiVersion: kubetask.io/v1alpha1
kind: Context
metadata:
  name: project-configs
  namespace: kubetask-system
spec:
  type: ConfigMap
  configMap:
    name: project-configs  # All keys become files
---
# Then create the Task referencing the Contexts
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: complex-task
  namespace: kubetask-system
spec:
  agentRef: claude
  description: "Refactor the authentication module"
  contexts:
    # Guide from Context CRD
    - name: refactoring-guide
      mountPath: /workspace/guide.md
    # Config directory from Context CRD
    - name: project-configs
      mountPath: /workspace/configs
```

### 3. Batch Operations with Helm

For running the same task across multiple targets, use Helm templating:

```yaml
# values.yaml
tasks:
  - name: update-service-a
    repo: service-a
  - name: update-service-b
    repo: service-b
  - name: update-service-c
    repo: service-c

# templates/tasks.yaml
{{- range .Values.tasks }}
---
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: {{ .name }}
spec:
  description: "Update dependencies for {{ .repo }}"
{{- end }}
```

```bash
# Generate and apply multiple tasks
helm template my-tasks ./chart | kubectl apply -f -
```

---

## kubectl Usage

### Task Operations

```bash
# Create a task
kubectl apply -f task.yaml

# List tasks
kubectl get tasks -n kubetask-system

# Watch task execution
kubectl get task update-service-a -n kubetask-system -w

# Check task status
kubectl get task update-service-a -o yaml

# View task logs
kubectl logs job/$(kubectl get task update-service-a -o jsonpath='{.status.jobName}') -n kubetask-system

# Stop a running task (gracefully stops and marks as Completed with logs preserved)
kubectl annotate task update-service-a kubetask.io/stop=true

# Resume a session (when sessionPersistence is enabled)
kubectl annotate task update-service-a kubetask.io/resume-session=true

# Access session Pod
kubectl exec -it update-service-a-session -c session -- /bin/bash

# Check session status
kubectl get task update-service-a -o jsonpath='{.status.sessionStatus}'

# List all session Pods
kubectl get pods -l kubetask.io/component=session -n kubetask-system

# Delete task
kubectl delete task update-service-a -n kubetask-system
```

### Workflow Operations

```bash
# Create a workflow template
kubectl apply -f workflow.yaml

# List workflow templates
kubectl get workflows -n kubetask-system

# Check workflow template details
kubectl get workflow ci-pipeline -o yaml

# Delete workflow template
kubectl delete workflow ci-pipeline -n kubetask-system
```

### WorkflowRun Operations

```bash
# Create a workflow run (referencing a template)
kubectl apply -f workflowrun.yaml

# List workflow runs
kubectl get workflowruns -n kubetask-system

# Watch workflow run execution
kubectl get workflowrun ci-pipeline-run-001 -n kubetask-system -w

# Check workflow run status
kubectl get workflowrun ci-pipeline-run-001 -o yaml

# View tasks created by workflow run
kubectl get tasks -l kubetask.io/workflow-run=ci-pipeline-run-001 -n kubetask-system

# View tasks in a specific stage
kubectl get tasks -l kubetask.io/workflow-run=ci-pipeline-run-001,kubetask.io/stage=testing -n kubetask-system

# Delete workflow run (also deletes all child Tasks)
kubectl delete workflowrun ci-pipeline-run-001 -n kubetask-system
```

### CronWorkflow Operations

```bash
# Create a scheduled workflow
kubectl apply -f cronworkflow.yaml

# List scheduled workflows
kubectl get cronworkflows -n kubetask-system

# Watch scheduled workflow status
kubectl get cronworkflow daily-ci -n kubetask-system -w

# Check scheduled workflow details
kubectl get cronworkflow daily-ci -o yaml

# Suspend a scheduled workflow
kubectl patch cronworkflow daily-ci -p '{"spec":{"suspend":true}}' --type=merge

# Resume a scheduled workflow
kubectl patch cronworkflow daily-ci -p '{"spec":{"suspend":false}}' --type=merge

# View workflow runs created by CronWorkflow
kubectl get workflowruns -l kubetask.io/cronworkflow=daily-ci -n kubetask-system

# Delete scheduled workflow
kubectl delete cronworkflow daily-ci -n kubetask-system
```

### WebhookTrigger Operations

```bash
# Create a webhook trigger
kubectl apply -f webhooktrigger.yaml

# List webhook triggers
kubectl get webhooktriggers -n kubetask-system
# Or use short name
kubectl get wht -n kubetask-system

# Check webhook trigger details
kubectl get webhooktrigger github-pr-review -o yaml

# View webhook URL
kubectl get webhooktrigger github-pr-review -o jsonpath='{.status.webhookURL}'

# View trigger statistics
kubectl get webhooktrigger github-pr-review -o jsonpath='{.status.totalTriggered}'

# List tasks created by webhook trigger
kubectl get tasks -l kubetask.io/webhook-trigger=github-pr-review -n kubetask-system

# Delete webhook trigger
kubectl delete webhooktrigger github-pr-review -n kubetask-system
```

### Agent Operations

```bash
# List agents
kubectl get agents -n kubetask-system

# Create agent
kubectl apply -f agent.yaml

# View agent details
kubectl get agent default -o yaml
```

---

## Benefits of Design

### 1. Simplicity

- **Core CRDs**: Task, Workflow, WorkflowRun, CronWorkflow, WebhookTrigger, and Agent
- **Clear separation**: WHAT (Task) vs WHEN (CronWorkflow/WebhookTrigger) vs HOW (Agent)
- **Kubernetes-native batch**: Use Helm/Kustomize for multiple Tasks
- **Follows K8s patterns**: CronWorkflow mirrors CronJob, WebhookTrigger mirrors event-driven patterns

### 2. Stability

- **Agent**: Won't change even if project renames
- **Core concepts**: Independent of project name

### 3. Flexibility

- Multiple context types (File, future: MCP)
- Directory mounts with ConfigMapRef
- Tools image for CLI tools

### 4. K8s Alignment

- **Agent**: Follows K8s Config pattern
- **Convention-based discovery**: K8s standard practice
- **Batch via Helm/Kustomize**: Cloud-native approach

---

## Summary

**API**:
- **Task** - primary API for single task execution
- **Workflow** - reusable multi-stage task template (no execution)
- **WorkflowRun** - workflow execution instance (stage-based DAG execution)
- **CronWorkflow** - scheduled WorkflowRun triggering (creates WorkflowRuns on cron schedule)
- **WebhookTrigger** - event-driven Task creation from webhooks (GitHub, GitLab, custom)
- **Agent** - stable, project-independent configuration
- **KubeTaskConfig** - system-level settings (TTL, lifecycle)

**Context Types** (via Context CRD):
- `Inline` - Content directly in YAML
- `ConfigMap` - Content from ConfigMap (single key or all keys as directory)
- `Git` - Content from Git repository with branch/tag/commit support
- `Runtime` - KubeTask platform awareness (environment variables, kubectl commands, system concepts)

**Task Lifecycle**:
- No retry on failure (AI tasks are non-idempotent)
- TTL-based automatic cleanup (default: 7 days)
- Human-in-the-loop debugging support (session sidecar)
- Session persistence for long-running work (save to PVC, resume via annotation)
- User-initiated stop via `kubetask.io/stop=true` annotation (graceful, logs preserved)
- Session resume via `kubetask.io/resume-session=true` annotation
- OwnerReference cascade deletion

**Batch Operations**:
- Use Helm, Kustomize, or other templating tools
- Kubernetes-native approach

**Advantages**:
- Simplified Architecture
- Native Integration with K8s tools
- Declarative Management (GitOps ready)
- Infrastructure Reuse
- Simplified Operations

---

**Status**: FINAL
**Date**: 2025-12-19
**Version**: v4.3 (WebhookTrigger Support)
**Maintainer**: KubeTask Team
