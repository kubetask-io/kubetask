# KubeOpenCode Architecture & API Design

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

KubeOpenCode brings Agentic AI capabilities into the Kubernetes ecosystem. By leveraging Kubernetes, it enables AI agents to be deployed as services, run in isolated virtual environments, and integrate with enterprise management and governance frameworks.

### Core Goals

- Use Kubernetes CRDs to define Task resources
- Use Controller pattern to manage resource lifecycle
- Execute tasks as Kubernetes Pods
- Seamless integration with Kubernetes ecosystem

### Key Advantages

- **Native Integration**: Works seamlessly with Helm, Kustomize, ArgoCD and other K8s tools
- **Declarative Management**: Use K8s resource definitions, supports GitOps
- **Infrastructure Reuse**: Logs, monitoring, auth/authz all leverage K8s capabilities
- **Simplified Operations**: Manage with standard K8s tools (kubectl, dashboard)
- **Batch Operations**: Use Helm/Kustomize to create multiple Tasks (Kubernetes-native approach)

### External Integrations

KubeOpenCode focuses on the core Task/Agent abstraction. For advanced features, integrate with external projects:

| Feature | Recommended Integration |
|---------|------------------------|
| Workflow orchestration | [Argo Workflows](https://argoproj.github.io/argo-workflows/) |
| Event-driven triggers | [Argo Events](https://argoproj.github.io/argo-events/) |
| Scheduled execution | Kubernetes CronJob or Argo CronWorkflows |

See `deploy/dogfooding/argo-events/` for examples of GitHub webhook integration using Argo Events that creates KubeOpenCode Tasks.

---

## API Design

### Resource Overview

| Resource | Purpose | Stability |
|----------|---------|-----------|
| **Task** | Single task execution (primary API) | Stable - semantic name |
| **Agent** | AI agent configuration (HOW to execute) | Stable - independent of project name |
| **KubeOpenCodeConfig** | System-level configuration | Stable - system settings |
| **ContextItem** | Inline context for AI agents (KNOW) | Stable - inline context only |

### Key Design Decisions

#### 1. Task as Primary API

**Rationale**: Simple, focused API for single task execution. For batch operations, use Helm/Kustomize to create multiple Tasks.

```yaml
apiVersion: kubeopencode.io/v1alpha1
kind: Task
```

#### 2. Agent (not KubeOpenCodeConfig)

**Rationale**:
- **Stable**: Independent of project name - won't change even if project renames
- **Semantic**: Reflects architecture philosophy: "Agent = AI + permissions + tools"
- **Clear**: Configures the agent environment for task execution

```yaml
apiVersion: kubeopencode.io/v1alpha1
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
- Pods are created with `RestartPolicy: Never` (no retry on failure)
- Pods use `restartPolicy: Never` (no container restart on failure)
- Task fails immediately when the agent container exits with non-zero code

**If retry is needed**, use external Kubernetes ecosystem components:
- **Argo Workflows**: DAG-based workflow with conditional retry logic
- **Tekton Pipelines**: CI/CD pipelines with result-based retry
- **Custom controllers**: Monitor Task status and create new Tasks based on validation results

### Resource Hierarchy

```
Task (single task execution)
├── TaskSpec
│   ├── description: *string         (syntactic sugar for /workspace/task.md)
│   ├── contexts: []ContextItem      (inline context definitions)
│   └── agentRef: *AgentReference    (cross-namespace Agent reference)
└── TaskExecutionStatus
    ├── phase: TaskPhase
    ├── podName: string
    ├── podNamespace: string         (where Pod runs - may differ from Task namespace)
    ├── startTime: Time
    ├── completionTime: Time
    └── conditions: []Condition

Agent (execution configuration)
└── AgentSpec
    ├── agentImage: string           (OpenCode init container image)
    ├── executorImage: string        (Main worker container image)
    ├── workspaceDir: string         (default: "/workspace")
    ├── command: []string
    ├── contexts: []ContextItem      (inline context definitions)
    ├── credentials: []Credential
    ├── podSpec: *AgentPodSpec
    ├── serviceAccountName: string
    ├── allowedNamespaces: []string  (restrict cross-namespace access)
    ├── maxConcurrentTasks: *int32   (limit concurrent Tasks, nil/0 = unlimited)
    └── quota: *QuotaConfig          (rate limiting for Task starts)
        ├── maxTaskStarts: int32     (max starts within window)
        └── windowSeconds: int32     (sliding window duration in seconds)

KubeOpenCodeConfig (system configuration)
└── KubeOpenCodeConfigSpec
    ├── systemImage: *SystemImageConfig       (internal KubeOpenCode components)
    │   ├── image: string                     (default: DefaultKubeOpenCodeImage)
    │   └── imagePullPolicy: PullPolicy       (default: IfNotPresent)
    └── cleanup: *CleanupConfig               (Task cleanup policies)
        ├── ttlSecondsAfterFinished: *int32   (TTL for finished Tasks, nil = disabled)
        └── maxRetainedTasks: *int32          (max Tasks to retain, nil = unlimited)
```

### Complete Type Definitions

```go
// Task represents a single task execution
type Task struct {
    Spec   TaskSpec
    Status TaskExecutionStatus
}

type TaskSpec struct {
    Description *string          // Syntactic sugar for /workspace/task.md
    Contexts    []ContextItem    // Inline context definitions
    AgentRef    *AgentReference  // Cross-namespace Agent reference
}

// AgentReference supports cross-namespace Agent references
type AgentReference struct {
    Name      string // Agent name (required)
    Namespace string // Agent namespace (optional, defaults to Task namespace)
}

// ContextItem defines inline context content
type ContextItem struct {
    Type      ContextType       // Text, ConfigMap, Git, or Runtime
    MountPath string            // Empty = write to .kubeopencode/context.md (ignored for Runtime)
    FileMode  *int32            // Optional file permission mode (e.g., 0755 for executable)
    Text      string            // Content when Type is Text
    ConfigMap *ConfigMapContext // ConfigMap when Type is ConfigMap
    Git       *GitContext       // Git repo when Type is Git
    Runtime   *RuntimeContext   // Platform awareness when Type is Runtime
}

type TaskExecutionStatus struct {
    Phase          TaskPhase
    PodName        string
    PodNamespace   string             // Where Pod runs (may differ from Task namespace)
    StartTime      *metav1.Time
    CompletionTime *metav1.Time
    Conditions     []metav1.Condition
}

type ContextType string
const (
    ContextTypeText      ContextType = "Text"
    ContextTypeConfigMap ContextType = "ConfigMap"
    ContextTypeGit       ContextType = "Git"
    ContextTypeRuntime   ContextType = "Runtime"
)

// RuntimeContext enables KubeOpenCode platform awareness for agents.
// When enabled, the controller injects a system prompt explaining:
// - The agent is running in a Kubernetes environment as a KubeOpenCode Task
// - Available environment variables (TASK_NAME, TASK_NAMESPACE, WORKSPACE_DIR)
// - How to query Task information via kubectl
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
    AgentImage         string           // OpenCode init container image (copies binary to /tools)
    ExecutorImage      string           // Main worker container image (runs tasks)
    WorkspaceDir       string           // Working directory (default: "/workspace")
    Command            []string         // Custom entrypoint command
    Contexts           []ContextItem    // Inline context definitions
    Credentials        []Credential
    PodSpec            *AgentPodSpec    // Pod configuration (labels, scheduling, runtime)
    ServiceAccountName string
    AllowedNamespaces  []string         // Restrict which namespaces can use this Agent
    MaxConcurrentTasks *int32           // Limit concurrent Tasks (nil/0 = unlimited)
}

// KubeOpenCodeConfig defines system-level configuration
type KubeOpenCodeConfig struct {
    Spec KubeOpenCodeConfigSpec
}

type KubeOpenCodeConfigSpec struct {
    SystemImage *SystemImageConfig // System image for internal components
    Cleanup     *CleanupConfig     // Task cleanup configuration
}

// SystemImageConfig configures the KubeOpenCode system image
type SystemImageConfig struct {
    Image           string            // System image (default: built-in DefaultKubeOpenCodeImage)
    ImagePullPolicy corev1.PullPolicy // Pull policy: Always/Never/IfNotPresent (default: IfNotPresent)
}

// CleanupConfig defines cleanup policies for completed/failed Tasks
type CleanupConfig struct {
    TTLSecondsAfterFinished *int32 // TTL for cleaning up finished Tasks (nil = disabled)
    MaxRetainedTasks        *int32 // Max completed Tasks to retain per namespace (nil = unlimited)
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
│              KubeOpenCode Controller (Operator)                 │
│  - Watch Task CRs                                           │
│  - Reconcile loop                                           │
│  - Create Kubernetes Pods for tasks                         │
│  - Update CR status fields                                  │
└─────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                   Kubernetes Pods                      │
│  - Each task runs as a separate Pod                     │
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
apiVersion: kubeopencode.io/v1alpha1
kind: Task
metadata:
  name: update-service-a
  namespace: kubeopencode-system
spec:
  # Simple task description (syntactic sugar for /workspace/task.md)
  description: |
    Update dependencies to latest versions.
    Run tests and create PR.

  # Inline context definitions
  contexts:
    - type: Text
      mountPath: /workspace/guides/standards.md
      text: |
        # Coding Standards
        - Use descriptive variable names
        - Write unit tests for all functions
    - type: ConfigMap
      configMap:
        name: security-policy
      # Empty mountPath = write to .kubeopencode/context.md with XML tags

  # Optional: Reference to Agent (defaults to "default")
  agentRef: my-agent

status:
  # Execution phase
  phase: Running  # Pending|Queued|Running|Completed|Failed

  # Kubernetes Pod name
  podName: update-service-a-xyz123

  # Start and end times
  startTime: "2025-01-18T10:00:00Z"
  completionTime: "2025-01-18T10:05:00Z"
```

**Field Description:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.description` | String | No | Task instruction (creates /workspace/task.md) |
| `spec.contexts` | []ContextItem | No | Inline context definitions (see below) |
| `spec.agentRef` | *AgentReference | No | Cross-namespace Agent reference (default: "default" in Task's namespace) |

**Status Field Description:**

| Field | Type | Description |
|-------|------|-------------|
| `status.phase` | TaskPhase | Execution phase: Pending\|Queued\|Running\|Completed\|Failed |
| `status.podName` | String | Kubernetes Pod name |
| `status.podNamespace` | String | Pod namespace (may differ from Task namespace for cross-namespace Agent) |
| `status.startTime` | Timestamp | Start time |
| `status.completionTime` | Timestamp | End time |

**ContextItem Types:**

Contexts are defined inline in Task or Agent specs:

1. **Text Context** - Inline text content:
```yaml
contexts:
  - type: Text
    mountPath: /workspace/guides/standards.md  # Optional
    text: |
      # Coding Standards
      - Use descriptive variable names
```

2. **ConfigMap Context** - Content from ConfigMap:
```yaml
contexts:
  - type: ConfigMap
    mountPath: /workspace/configs  # Optional
    configMap:
      name: my-configs
      key: config.md  # Optional: specific key
```

3. **Git Context** - Content from Git repository:
```yaml
contexts:
  - type: Git
    mountPath: /workspace/repo
    git:
      repository: https://github.com/org/contexts
      path: guides/
      ref: main
```

4. **Runtime Context** - KubeOpenCode platform awareness:
```yaml
contexts:
  - type: Runtime
    runtime: {}  # No fields - content is generated by controller
```

### Context System

Contexts provide additional information to AI agents during task execution. They are defined inline in Task or Agent specs using the `ContextItem` structure.

**Context Types:**

| Type | Description |
|------|-------------|
| `Text` | Inline text content directly in YAML |
| `ConfigMap` | Content from a Kubernetes ConfigMap |
| `Git` | Content cloned from a Git repository |
| `Runtime` | KubeOpenCode platform awareness (auto-generated by controller) |
| `URL` | Content fetched from a remote HTTP/HTTPS URL at task execution time |

**ContextItem Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | No | Optional identifier for logging, debugging, and XML tag generation |
| `description` | string | No | Human-readable documentation for the context |
| `optional` | *bool | No | If true, task proceeds even if context cannot be resolved |
| `type` | ContextType | Yes | Type of context: Text, ConfigMap, Git, Runtime, or URL |
| `mountPath` | string | No | Where to mount (empty = write to .kubeopencode/context.md) |
| `fileMode` | *int32 | No | File permission mode (e.g., 0755 for executables) |
| `text` | string | When type=Text | Text content |
| `configMap` | ConfigMapContext | When type=ConfigMap | Reference to ConfigMap |
| `git` | GitContext | When type=Git | Content from Git repository |
| `runtime` | RuntimeContext | When type=Runtime | Platform awareness (no fields - content is generated by controller) |
| `url` | URLContext | When type=URL | Remote URL to fetch content from |

**Important Notes:**

- **Empty MountPath behavior**: When mountPath is empty, content is written to `${WORKSPACE_DIR}/.kubeopencode/context.md` with XML tags. OpenCode loads this via `OPENCODE_CONFIG_CONTENT` env var, avoiding conflicts with repository's `AGENTS.md`
- **Runtime context**: Provides KubeOpenCode platform awareness to agents, explaining environment variables, kubectl commands, and system concepts
- **Path resolution**: Relative paths are prefixed with workspaceDir; absolute paths are used as-is
- **URL context**: Fetches content at task execution time via an init container. Requires `mountPath` to be specified
- **Cross-namespace ConfigMap**: When Task references a cross-namespace Agent, ConfigMap contexts are read from Task's namespace and embedded into the execution namespace

**Context Priority (lowest to highest):**

1. Agent.contexts (array order)
2. Task.contexts (array order)
3. Task.description (becomes /workspace/task.md)

### Agent (Execution Configuration)

Agent defines the AI agent configuration for task execution.

KubeOpenCode uses a **two-container pattern**:
1. **Init Container** (OpenCode image): Copies OpenCode binary to `/tools` shared volume
2. **Worker Container** (Executor image): Uses `/tools/opencode` to run AI tasks

```yaml
apiVersion: kubeopencode.io/v1alpha1
kind: Agent
metadata:
  name: default  # Convention: "default" is used when no agentRef is specified
  namespace: kubeopencode-system
spec:
  # OpenCode init container image (optional, has default)
  agentImage: quay.io/kubeopencode/kubeopencode-agent-opencode:latest

  # Executor container image (worker that runs OpenCode)
  executorImage: quay.io/kubeopencode/kubeopencode-agent-devbox:latest

  # Optional: Working directory (default: "/workspace")
  workspaceDir: /workspace

  # Optional: Custom entrypoint command (uses OpenCode from /tools)
  command: ["sh", "-c", "/tools/opencode run --format json \"$(cat /workspace/task.md)\""]

  # Optional: Inline contexts (applied to all tasks using this agent)
  contexts:
    - type: Text
      text: |
        # Coding Standards
        - Use descriptive variable names
        - Write unit tests for all functions
    - type: ConfigMap
      configMap:
        name: org-security-policy

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

  # Optional: Limit concurrent Tasks using this Agent
  maxConcurrentTasks: 3

  # Required: ServiceAccount for agent pods
  serviceAccountName: kubeopencode-agent
```

**Field Description:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.agentImage` | String | No | OpenCode init container image (copies binary to /tools) |
| `spec.executorImage` | String | No | Main worker container image (runs tasks) |
| `spec.workspaceDir` | String | No | Working directory (default: "/workspace") |
| `spec.command` | []String | No | Custom entrypoint command |
| `spec.contexts` | []ContextItem | No | Inline contexts (applied to all tasks) |
| `spec.credentials` | []Credential | No | Secrets as env vars or file mounts |
| `spec.podSpec` | *AgentPodSpec | No | Advanced Pod configuration (labels, scheduling, runtimeClass) |
| `spec.allowedNamespaces` | []String | No | Restrict which namespaces can use this Agent (empty = all allowed) |
| `spec.maxConcurrentTasks` | *int32 | No | Limit concurrent Tasks (nil/0 = unlimited) |
| `spec.quota` | *QuotaConfig | No | Rate limiting for Task starts |
| `spec.quota.maxTaskStarts` | int32 | Yes (if quota set) | Maximum Task starts within the window |
| `spec.quota.windowSeconds` | int32 | Yes (if quota set) | Sliding window duration in seconds (60-86400) |
| `spec.serviceAccountName` | String | Yes | ServiceAccount for agent pods |

**Task Stop:**

Running Tasks can be stopped by setting the `kubeopencode.io/stop=true` annotation:

```bash
kubectl annotate task my-task kubeopencode.io/stop=true
```

When this annotation is detected:
- The controller deletes the Pod (with graceful termination)
- Kubernetes sends SIGTERM to all running Pods, triggering graceful shutdown
- Pod is deleted after termination (logs are not preserved)
- Task status is set to `Completed` with a `Stopped` condition

### Cross-Namespace Task/Agent Separation

KubeOpenCode supports cross-namespace Agent references, enabling separation of concerns:

- **Platform teams** manage Agents with credentials in a dedicated namespace
- **Dev teams** create Tasks in their own namespaces, referencing shared Agents
- **Pods run in Agent's namespace**, keeping credentials isolated from Task creators

```
┌─────────────────────────────────────────────────────────────────┐
│                    Task Namespace (dev-team-a)                  │
│  ┌──────────┐                                                   │
│  │   Task   │  agentRef:                                        │
│  │          │    name: opencode-agent                           │
│  │          │    namespace: platform-agents                     │
│  └──────────┘                                                   │
│       │                                                         │
└───────│─────────────────────────────────────────────────────────┘
        │ references
        ▼
┌─────────────────────────────────────────────────────────────────┐
│                  Agent Namespace (platform-agents)              │
│  ┌──────────┐     ┌──────────┐     ┌──────────┐                │
│  │  Agent   │     │ Secrets  │     │   Pod    │◄── runs here   │
│  │          │     │(API keys)│     │          │                │
│  └──────────┘     └──────────┘     └──────────┘                │
└─────────────────────────────────────────────────────────────────┘
```

**Example - Cross-Namespace Setup:**

```yaml
# In platform-agents namespace - managed by Infra team
apiVersion: kubeopencode.io/v1alpha1
kind: Agent
metadata:
  name: opencode-agent
  namespace: platform-agents
spec:
  # Restrict which namespaces can use this Agent
  allowedNamespaces:
    - "dev-*"
    - "staging"
  agentImage: quay.io/kubeopencode/kubeopencode-agent-opencode:latest
  executorImage: quay.io/kubeopencode/kubeopencode-agent-devbox:latest
  serviceAccountName: kubeopencode-agent
  workspaceDir: /workspace
  command:
    - sh
    - -c
    - /tools/opencode run --format json "$(cat ${WORKSPACE_DIR}/task.md)"
  credentials:
    - name: anthropic-key
      secretRef:
        name: anthropic-credentials
        key: api-key
      env: ANTHROPIC_API_KEY
---
# In dev-team-a namespace - created by Dev team
apiVersion: kubeopencode.io/v1alpha1
kind: Task
metadata:
  name: fix-bug-123
  namespace: dev-team-a
spec:
  # Reference Agent in different namespace
  agentRef:
    name: opencode-agent
    namespace: platform-agents
  description: "Fix the bug in authentication module"
```

**Result:**
- Task exists in `dev-team-a` namespace
- Pod runs in `platform-agents` namespace (where Agent lives)
- Credentials (anthropic-credentials Secret) stay in `platform-agents` - never exposed to dev team
- Task status shows `podNamespace: platform-agents`

**AllowedNamespaces:**

The `allowedNamespaces` field supports glob patterns:
- `"dev-*"` - matches `dev-team-a`, `dev-team-b`, etc.
- `"staging"` - exact match
- Empty list (default) - all namespaces are allowed

When a Task in a non-allowed namespace references this Agent, it fails with an error condition.

---

## Agent Configuration

### Agent Image Discovery

KubeOpenCode uses a **two-container pattern** for AI task execution:

1. **Init Container** (`agentImage`): Copies OpenCode binary to `/tools` shared volume
2. **Worker Container** (`executorImage`): Uses `/tools/opencode` to run AI tasks

### Image Resolution

| Field | Container | Default |
|-------|-----------|---------|
| `agentImage` | Init Container (OpenCode) | `quay.io/kubeopencode/kubeopencode-agent-opencode:latest` |
| `executorImage` | Worker Container | `quay.io/kubeopencode/kubeopencode-agent-devbox:latest` |

**Backward Compatibility:**
- If only `agentImage` is set (legacy): it's used as the executor image, default OpenCode image is used for init container
- If both are set: `agentImage` for init container, `executorImage` for worker container

### How It Works

The controller:
1. Looks up the Agent referenced by `agentRef` (defaults to "default")
2. Resolves `agentImage` and `executorImage` with backward compatibility
3. Generates a Pod with:
   - `opencode-init` init container (copies OpenCode binary to `/tools`)
   - Worker container with the executor image
   - Labels for tracking (`kubeopencode.io/task`)
   - Environment variables (`TASK_NAME`, `TASK_NAMESPACE`)
   - Owner references for garbage collection
   - ServiceAccount from Agent spec

### Concurrency Control

Agents can limit the number of concurrent Tasks to prevent overwhelming backend AI services with rate limits:

```yaml
apiVersion: kubeopencode.io/v1alpha1
kind: Agent
metadata:
  name: opencode-agent
spec:
  # OpenCode init container (optional, has default)
  agentImage: quay.io/kubeopencode/kubeopencode-agent-opencode:latest
  # Executor container (optional, has default)
  executorImage: quay.io/kubeopencode/kubeopencode-agent-devbox:latest
  command: ["sh", "-c", "/tools/opencode run --format json \"$(cat /workspace/task.md)\""]
  serviceAccountName: kubeopencode-agent
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

### Quota (Rate Limiting)

In addition to concurrent Task limits, Agents support rate limiting via `quota`:

```yaml
apiVersion: kubeopencode.io/v1alpha1
kind: Agent
metadata:
  name: rate-limited-agent
spec:
  agentImage: quay.io/kubeopencode/kubeopencode-agent-opencode:latest
  executorImage: quay.io/kubeopencode/kubeopencode-agent-devbox:latest
  workspaceDir: /workspace
  command: ["sh", "-c", "/tools/opencode run \"$(cat /workspace/task.md)\""]
  serviceAccountName: kubeopencode-agent
  quota:
    maxTaskStarts: 10     # Maximum 10 task starts
    windowSeconds: 3600   # Per hour (sliding window)
```

**Quota vs MaxConcurrentTasks:**

| Feature | `maxConcurrentTasks` | `quota` |
|---------|----------------------|---------|
| What it limits | Simultaneous running Tasks | Rate of new Task starts |
| Time component | No (instant check) | Yes (sliding window) |
| State tracking | None (counts Running Tasks) | `Agent.status.taskStartHistory` |
| Queued Reason | `AgentAtCapacity` | `QuotaExceeded` |
| Use case | Limit resource usage | API rate limiting |

Both can be used together for comprehensive control.

**Agent Status with Quota:**

When quota is configured, the Agent's status tracks recent Task starts:

```yaml
status:
  taskStartHistory:
    - taskName: "task-1"
      taskNamespace: "default"
      startTime: "2024-01-15T10:00:00Z"
    - taskName: "task-2"
      taskNamespace: "default"
      startTime: "2024-01-15T10:05:00Z"
```

Records are automatically pruned when they fall outside the sliding window.

### Server Mode (Persistent OpenCode Server)

Agents support two execution modes:

| Mode | Description | Use Case |
|------|-------------|----------|
| **Pod mode** (default) | Creates a new Pod for each Task | Standard task execution |
| **Server mode** | Runs a persistent OpenCode server (Deployment + Service) | Long-running agents, shared context |

Server mode is enabled by adding `serverConfig` to the Agent spec:

```yaml
apiVersion: kubeopencode.io/v1alpha1
kind: Agent
metadata:
  name: slack-agent
  namespace: platform-agents
spec:
  executorImage: quay.io/kubeopencode/kubeopencode-agent-devbox:latest
  workspaceDir: /workspace
  serviceAccountName: kubeopencode-agent

  # Presence of serverConfig enables Server mode
  serverConfig:
    port: 4096                    # OpenCode server port (default: 4096)

  # Resource requirements (applies to both Pod and Server modes)
  podSpec:
    resources:
      requests:
        memory: "512Mi"
        cpu: "500m"

  # Concurrency and quota limits apply equally to Server mode
  maxConcurrentTasks: 10
```

**How Server Mode Works:**

```
Agent Created (with serverConfig)
    │
    ▼
Agent Controller
    ├── Creates Deployment (opencode serve)
    └── Creates Service (ClusterIP)

Task Created (referencing Server-mode Agent)
    │
    ▼
Task Controller
    ├── Creates Pod with command: opencode run --attach <server-url> "task"
    ├── Standard Pod status tracking (same as Pod mode)
    └── Logs available via kubectl logs
```

**ServerConfig Fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `port` | int32 | 4096 | Port for OpenCode server |

**Server Mode Status:**

When an Agent is in Server mode, its status includes:

```yaml
status:
  serverStatus:
    deploymentName: slack-agent-server
    serviceName: slack-agent
    url: http://slack-agent.platform-agents.svc.cluster.local:4096
    readyReplicas: 1
  conditions:
    - type: ServerReady
      status: "True"
      reason: DeploymentReady
    - type: ServerHealthy
      status: "True"
      reason: DeploymentHealthy
```

**Key Differences:**

| Aspect | Pod Mode | Server Mode |
|--------|----------|-------------|
| Lifecycle | Ephemeral Pod per Task | Persistent Deployment + Pod per Task |
| Command | `opencode run "task"` | `opencode run --attach <url> "task"` |
| Cold start | Yes | No (server already running) |
| Context sharing | None | Shared across Tasks via server |
| Logs | `kubectl logs <pod>` | `kubectl logs <pod>` (same) |
| Task API | Same | Same |

**Task API Unchanged**: Tasks referencing Server-mode Agents use the same API as Pod-mode. The execution mode is an Agent-level configuration, transparent to Task authors.

---

## System Configuration

### KubeOpenCodeConfig (System-level Configuration)

KubeOpenCodeConfig provides cluster or namespace-level settings for container image configuration and Task cleanup policies.

```yaml
apiVersion: kubeopencode.io/v1alpha1
kind: KubeOpenCodeConfig
metadata:
  name: default
  namespace: kubeopencode-system
spec:
  # System image configuration for internal KubeOpenCode components
  # (git-init, context-init containers)
  systemImage:
    image: quay.io/kubeopencode/kubeopencode:latest  # Default system image
    imagePullPolicy: Always  # Always/Never/IfNotPresent (default: IfNotPresent)

  # Task cleanup configuration (optional)
  cleanup:
    # Delete completed/failed Tasks after 1 hour
    ttlSecondsAfterFinished: 3600
    # Keep at most 100 completed Tasks per namespace
    maxRetainedTasks: 100
```

**Field Description:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.systemImage.image` | string | No | System image for internal components (default: built-in DefaultKubeOpenCodeImage) |
| `spec.systemImage.imagePullPolicy` | string | No | Pull policy for system containers: Always, Never, IfNotPresent (default: IfNotPresent) |
| `spec.cleanup.ttlSecondsAfterFinished` | int32 | No | TTL in seconds for cleaning up finished Tasks. Tasks are deleted after this duration from CompletionTime. |
| `spec.cleanup.maxRetainedTasks` | int32 | No | Maximum number of completed/failed Tasks to retain per namespace. Oldest Tasks (by CompletionTime) are deleted first. |

**Image Pull Policy:**

Setting `imagePullPolicy: Always` is recommended when:
- Using `:latest` tags in development/staging environments
- Nodes may have cached old images that differ from registry versions
- Frequent image updates are expected

The `systemImage` configuration affects all internal KubeOpenCode containers:
- `git-init`: Clones Git repositories for Context
- `context-init`: Copies ConfigMap content to writable workspace

**Task Cleanup:**

The cleanup configuration enables automatic garbage collection of completed/failed Tasks:

- **TTL-based cleanup**: Tasks are deleted after `ttlSecondsAfterFinished` seconds from completion
- **Retention-based cleanup**: Only the most recent `maxRetainedTasks` completed Tasks are retained
- **Combined**: Both policies can be used together. TTL is checked first, then retention count
- **Cascading deletion**: Deleting a Task automatically deletes its associated Pod and ConfigMap

Cleanup is disabled by default. When `KubeOpenCodeConfig` is not present or `cleanup` is not specified, Tasks are never automatically deleted

---

## Complete Examples

### 1. Simple Task Execution

```yaml
# Create Agent
apiVersion: kubeopencode.io/v1alpha1
kind: Agent
metadata:
  name: default
  namespace: kubeopencode-system
spec:
  # OpenCode init container (optional, uses default if not specified)
  agentImage: quay.io/kubeopencode/kubeopencode-agent-opencode:latest
  # Executor image (worker container)
  executorImage: quay.io/kubeopencode/kubeopencode-agent-devbox:latest
  # Command uses OpenCode from /tools (injected by init container)
  command: ["sh", "-c", "/tools/opencode run --format json \"$(cat /workspace/task.md)\""]
  workspaceDir: /workspace
  serviceAccountName: kubeopencode-agent
---
# Create Task
apiVersion: kubeopencode.io/v1alpha1
kind: Task
metadata:
  name: update-service-a
  namespace: kubeopencode-system
spec:
  description: |
    Update dependencies to latest versions.
    Run tests and create PR.
```

### 2. Task with Multiple Contexts

```yaml
apiVersion: kubeopencode.io/v1alpha1
kind: Task
metadata:
  name: complex-task
  namespace: kubeopencode-system
spec:
  agentRef: default
  description: "Refactor the authentication module"
  contexts:
    # ConfigMap context (specific key)
    - type: ConfigMap
      mountPath: /workspace/guide.md
      configMap:
        name: guides
        key: refactoring-guide.md
    # ConfigMap context (all keys as directory)
    - type: ConfigMap
      mountPath: /workspace/configs
      configMap:
        name: project-configs
    # Git context
    - type: Git
      mountPath: /workspace/repo
      git:
        repository: https://github.com/org/repo
        ref: main
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
apiVersion: kubeopencode.io/v1alpha1
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
kubectl get tasks -n kubeopencode-system

# Watch task execution
kubectl get task update-service-a -n kubeopencode-system -w

# Check task status
kubectl get task update-service-a -o yaml

# View task logs
kubectl logs $(kubectl get task update-service-a -o jsonpath='{.status.podName}') -n kubeopencode-system

# Stop a running task (gracefully stops and marks as Completed with logs preserved)
kubectl annotate task update-service-a kubeopencode.io/stop=true

# Delete task
kubectl delete task update-service-a -n kubeopencode-system
```

### Agent Operations

```bash
# List agents
kubectl get agents -n kubeopencode-system

# Create agent
kubectl apply -f agent.yaml

# View agent details
kubectl get agent default -o yaml
```

---

## Summary

**API**:
- **Task** - primary API for single task execution
- **Agent** - stable, project-independent configuration
- **KubeOpenCodeConfig** - system-level settings (systemImage, cleanup)

**Context Types** (via ContextItem):
- `Text` - Content directly in YAML
- `ConfigMap` - Content from ConfigMap (single key or all keys as directory)
- `Git` - Content from Git repository with branch/tag/commit support
- `Runtime` - KubeOpenCode platform awareness (environment variables, kubectl commands, system concepts)
- `URL` - Content fetched from remote HTTP/HTTPS URL at task execution time

**Cross-Namespace Support**:
- Tasks can reference Agents in different namespaces via `agentRef: {name, namespace}`
- Pods run in Agent's namespace, keeping credentials isolated from Task creators
- `allowedNamespaces` field on Agent restricts which namespaces can use it (supports glob patterns)
- Finalizers ensure cross-namespace Pod cleanup on Task deletion

**Task Lifecycle**:
- No retry on failure (AI tasks are non-idempotent)
- User-initiated stop via `kubeopencode.io/stop=true` annotation (graceful, Pod deleted)
- Finalizer-based cleanup for Pod and ConfigMap (works for both same-namespace and cross-namespace)

**Batch Operations**:
- Use Helm, Kustomize, or other templating tools
- Kubernetes-native approach

**Event-Driven Triggers**:
- Use [Argo Events](https://argoproj.github.io/argo-events/) for webhook-driven Task creation
- See `deploy/dogfooding/argo-events/` for examples

**Workflow Orchestration**:
- Use [Argo Workflows](https://argoproj.github.io/argo-workflows/) for multi-stage task orchestration
- KubeOpenCode Tasks can be triggered from Argo Workflow steps

**Advantages**:
- Simplified Architecture
- Native Integration with K8s tools
- Declarative Management (GitOps ready)
- Infrastructure Reuse
- Simplified Operations

---

**Status**: FINAL
**Date**: 2026-01-17
**Version**: v0.1.0
**Maintainer**: KubeOpenCode Team
