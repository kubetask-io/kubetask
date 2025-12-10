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
| **CronTask** | Scheduled/recurring task execution | Stable - follows K8s CronJob pattern |
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

### Resource Hierarchy

```
Task (single task execution)
├── TaskSpec
│   ├── contexts: []Context
│   └── agentRef: string
└── TaskExecutionStatus
    ├── phase: TaskPhase
    ├── jobName: string
    ├── startTime: Time
    └── completionTime: Time

CronTask (scheduled task execution)
├── CronTaskSpec
│   ├── schedule: string (cron expression)
│   ├── concurrencyPolicy: ConcurrencyPolicy
│   ├── suspend: *bool
│   ├── successfulTasksHistoryLimit: *int32
│   ├── failedTasksHistoryLimit: *int32
│   └── taskTemplate: TaskTemplateSpec
└── CronTaskStatus
    ├── active: []ObjectReference
    ├── lastScheduleTime: *Time
    ├── lastSuccessfulTime: *Time
    └── conditions: []Condition

Agent (execution configuration)
└── AgentSpec
    ├── agentImage: string
    ├── command: []string
    ├── humanInTheLoop: *HumanInTheLoop
    ├── defaultContexts: []Context
    ├── credentials: []Credential
    ├── podLabels: map[string]string
    ├── scheduling: *PodScheduling
    └── serviceAccountName: string

KubeTaskConfig (system configuration)
└── KubeTaskConfigSpec
    └── taskLifecycle: *TaskLifecycleConfig
        └── ttlSecondsAfterFinished: *int32
```

### Complete Type Definitions

```go
// Task represents a single task execution
type Task struct {
    Spec   TaskSpec
    Status TaskExecutionStatus
}

type TaskSpec struct {
    Contexts []Context
    AgentRef string  // Reference to Agent
}

type TaskExecutionStatus struct {
    Phase          TaskPhase
    JobName        string
    StartTime      *metav1.Time
    CompletionTime *metav1.Time
    Conditions     []metav1.Condition
}

// CronTask represents scheduled task execution
type CronTask struct {
    Spec   CronTaskSpec
    Status CronTaskStatus
}

type CronTaskSpec struct {
    Schedule                    string            // Cron expression (e.g., "0 9 * * *")
    ConcurrencyPolicy           ConcurrencyPolicy // Allow|Forbid|Replace
    Suspend                     *bool             // Suspend scheduling
    SuccessfulTasksHistoryLimit *int32            // Keep N successful tasks (default: 3)
    FailedTasksHistoryLimit     *int32            // Keep N failed tasks (default: 1)
    TaskTemplate                TaskTemplateSpec  // Template for created Tasks
}

type ConcurrencyPolicy string
const (
    AllowConcurrent   ConcurrencyPolicy = "Allow"   // Allow concurrent runs
    ForbidConcurrent  ConcurrencyPolicy = "Forbid"  // Skip if previous running
    ReplaceConcurrent ConcurrencyPolicy = "Replace" // Cancel previous, start new
)

type TaskTemplateSpec struct {
    metav1.ObjectMeta  // Labels and annotations for created Tasks
    Spec TaskSpec      // TaskSpec to use
}

type CronTaskStatus struct {
    Active             []corev1.ObjectReference // Currently running Tasks
    LastScheduleTime   *metav1.Time             // Last scheduled time
    LastSuccessfulTime *metav1.Time             // Last successful completion
    Conditions         []metav1.Condition
}

// Agent defines the AI agent configuration
type Agent struct {
    Spec AgentSpec
}

type AgentSpec struct {
    AgentImage         string
    Command            []string          // Custom entrypoint command
    HumanInTheLoop     *HumanInTheLoop   // Keep container alive after task completion
    DefaultContexts    []Context
    Credentials        []Credential
    PodLabels          map[string]string
    Scheduling         *PodScheduling
    ServiceAccountName string
}

// HumanInTheLoop keeps container running after task completion for debugging
type HumanInTheLoop struct {
    Enabled          bool    // Enable human-in-the-loop mode
    KeepAliveSeconds *int32  // How long to keep container alive (default: 3600)
}

// KubeTaskConfig defines system-level configuration
type KubeTaskConfig struct {
    Spec KubeTaskConfigSpec
}

type KubeTaskConfigSpec struct {
    TaskLifecycle *TaskLifecycleConfig
}

type TaskLifecycleConfig struct {
    TTLSecondsAfterFinished *int32  // TTL for completed/failed tasks (default: 604800 = 7 days)
}

// Context system
type Context struct {
    Type ContextType
    File *FileContext
}

type ContextType string
const (
    ContextTypeFile ContextType = "File"
)

type FileContext struct {
    FilePath string  // For single file
    DirPath  string  // For directory (with ConfigMapRef)
    Source   FileSource
}

type FileSource struct {
    Inline          *string
    ConfigMapKeyRef *ConfigMapKeySelector
    ConfigMapRef    *ConfigMapReference  // For directory mount
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
  # Contexts defines what this task operates on
  contexts:
    # File context - task description (inline)
    - type: File
      file:
        filePath: /workspace/task.md
        source:
          inline: |
            Update dependencies to latest versions.
            Run tests and create PR.

    # File context - from ConfigMap key
    - type: File
      file:
        filePath: /workspace/guide.md
        source:
          configMapKeyRef:
            name: workflow-guides
            key: standard-pr-workflow.md

    # Directory context - mount entire ConfigMap as directory
    - type: File
      file:
        dirPath: /workspace/configs
        source:
          configMapRef:
            name: my-configs  # All keys become files

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
| `spec.contexts` | []Context | Yes | List of contexts (files) |
| `spec.agentRef` | String | No | Reference to Agent (default: "default") |

**Status Field Description:**

| Field | Type | Description |
|-------|------|-------------|
| `status.phase` | TaskPhase | Execution phase: Pending\|Running\|Completed\|Failed |
| `status.jobName` | String | Kubernetes Job name |
| `status.startTime` | Timestamp | Start time |
| `status.completionTime` | Timestamp | End time |

**Context Types:**

1. **File Context (single file)**:
```yaml
type: File
file:
  filePath: /workspace/task.md
  source:
    inline: "Task description"
    # OR
    configMapKeyRef:
      name: configs
      key: task.md
```

2. **File Context (directory)**:
```yaml
type: File
file:
  dirPath: /workspace/configs
  source:
    configMapRef:
      name: my-configs  # All keys become files in directory
```

### CronTask (Scheduled Execution)

CronTask creates Task resources on a schedule, similar to how Kubernetes CronJob creates Jobs.

```yaml
apiVersion: kubetask.io/v1alpha1
kind: CronTask
metadata:
  name: daily-report
  namespace: kubetask-system
spec:
  # Cron schedule (required)
  schedule: "0 9 * * *"  # Every day at 9:00 AM

  # Concurrency policy (optional, default: Forbid)
  # - Allow: run concurrent Tasks
  # - Forbid: skip if previous Task still running
  # - Replace: cancel previous Task and start new one
  concurrencyPolicy: Forbid

  # Suspend scheduling (optional, default: false)
  suspend: false

  # History limits (optional)
  successfulTasksHistoryLimit: 3  # Keep 3 successful Tasks
  failedTasksHistoryLimit: 1      # Keep 1 failed Task

  # Task template (required)
  taskTemplate:
    metadata:
      labels:
        app: daily-report
    spec:
      contexts:
        - type: File
          file:
            filePath: /workspace/task.md
            source:
              inline: "Generate daily status report"
      agentRef: claude

status:
  # Currently running Tasks
  active:
    - name: daily-report-1733846400
      namespace: kubetask-system

  # Last scheduled time
  lastScheduleTime: "2025-12-10T09:00:00Z"

  # Last successful completion
  lastSuccessfulTime: "2025-12-09T09:05:00Z"

  # Conditions
  conditions:
    - type: Scheduled
      status: "True"
      reason: TaskCreated
      message: "Created Task daily-report-1733846400"
```

**Field Description:**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `spec.schedule` | String | Yes | - | Cron expression (e.g., "0 9 * * *") |
| `spec.concurrencyPolicy` | String | No | Forbid | Allow\|Forbid\|Replace |
| `spec.suspend` | Bool | No | false | Suspend scheduling |
| `spec.successfulTasksHistoryLimit` | Int32 | No | 3 | Number of successful Tasks to keep |
| `spec.failedTasksHistoryLimit` | Int32 | No | 1 | Number of failed Tasks to keep |
| `spec.taskTemplate` | TaskTemplateSpec | Yes | - | Template for created Tasks |

**Concurrency Policies:**

| Policy | Behavior |
|--------|----------|
| `Allow` | Allow multiple Tasks to run concurrently |
| `Forbid` | Skip this run if previous Task still running (default) |
| `Replace` | Cancel the currently running Task and start a new one |

**Task Naming:**

Created Tasks are named `{crontask-name}-{unix-timestamp}` (e.g., `daily-report-1733846400`).

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
  agentImage: quay.io/myorg/claude-agent:v1.0

  # Optional: Custom entrypoint command (required for humanInTheLoop)
  command: ["sh", "-c", "gemini --yolo -p \"$(cat /workspace/task.md)\""]

  # Optional: Keep container alive after task completion for debugging
  humanInTheLoop:
    enabled: true
    keepAliveSeconds: 3600  # Default: 3600 (1 hour)

  # Optional: Default contexts for all tasks using this agent
  defaultContexts:
    - type: File
      file:
        filePath: /workspace/org-guidelines.md
        source:
          configMapKeyRef:
            name: org-configs
            key: guidelines.md

  # Optional: Credentials (secrets as env vars or file mounts)
  credentials:
    - name: github-token
      secretRef:
        name: github-creds
        key: token
      env: GITHUB_TOKEN

    - name: ssh-key
      secretRef:
        name: ssh-keys
        key: id_rsa
      mountPath: /home/agent/.ssh/id_rsa
      fileMode: 0400

  # Optional: Pod labels for NetworkPolicy, monitoring, etc.
  podLabels:
    network-policy: agent-restricted

  # Optional: Scheduling constraints
  scheduling:
    nodeSelector:
      kubernetes.io/os: linux
    tolerations:
      - key: "dedicated"
        operator: "Equal"
        value: "ai-workload"
        effect: "NoSchedule"

  # Required: ServiceAccount for agent pods
  serviceAccountName: kubetask-agent
```

**Field Description:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.agentImage` | String | No | Agent container image |
| `spec.command` | []String | No | Custom entrypoint command (required for humanInTheLoop) |
| `spec.humanInTheLoop` | *HumanInTheLoop | No | Keep container alive after completion |
| `spec.defaultContexts` | []Context | No | Default contexts for all tasks |
| `spec.credentials` | []Credential | No | Secrets as env vars or file mounts |
| `spec.podLabels` | map[string]string | No | Additional pod labels |
| `spec.scheduling` | *PodScheduling | No | Node selector, tolerations, affinity |
| `spec.serviceAccountName` | String | Yes | ServiceAccount for agent pods |

**Human-in-the-Loop:**

When `humanInTheLoop.enabled` is true, the controller wraps the `command` with a sleep to keep the container running after task completion. This allows users to `kubectl exec` into the container for debugging or review.

```yaml
humanInTheLoop:
  enabled: true
  keepAliveSeconds: 3600  # Keep alive for 1 hour (default)
```

**Important:** When `humanInTheLoop` is enabled, you MUST also specify `command`. The controller wraps the command to add the sleep behavior.

---

## Agent Configuration

### Agent Image Discovery

Controller determines the agent image in this priority order:

1. **Agent.spec.agentImage** (from referenced Agent)
2. **Built-in default** (fallback) - `quay.io/zhaoxue/kubetask-agent-gemini:latest`

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

When a Task references an Agent with `defaultContexts`, contexts are merged:

1. **Agent.defaultContexts** (base layer, lowest priority)
2. **Task.contexts** (task-specific, highest priority)

---

## System Configuration

### KubeTaskConfig (System-level Configuration)

KubeTaskConfig provides cluster or namespace-level settings for task lifecycle management.

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
```

**Field Description:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.taskLifecycle.ttlSecondsAfterFinished` | int32 | No | TTL in seconds for completed/failed tasks (default: 604800 = 7 days) |

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
  agentImage: quay.io/myorg/claude-agent:v1.0
  serviceAccountName: kubetask-agent
---
# Create Task
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: update-service-a
  namespace: kubetask-system
spec:
  contexts:
    - type: File
      file:
        filePath: /workspace/task.md
        source:
          inline: |
            Update dependencies to latest versions.
            Run tests and create PR.
```

### 2. Task with Multiple Context Sources

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: complex-task
  namespace: kubetask-system
spec:
  agentRef: claude-workspace
  contexts:
    # Inline task description
    - type: File
      file:
        filePath: /workspace/task.md
        source:
          inline: "Refactor the authentication module"

    # Guide from ConfigMap
    - type: File
      file:
        filePath: /workspace/guide.md
        source:
          configMapKeyRef:
            name: guides
            key: refactoring-guide.md

    # Multiple config files as directory
    - type: File
      file:
        dirPath: /workspace/configs
        source:
          configMapRef:
            name: project-configs
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
  contexts:
    - type: File
      file:
        filePath: /workspace/task.md
        source:
          inline: "Update dependencies for {{ .repo }}"
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

# Delete task
kubectl delete task update-service-a -n kubetask-system
```

### CronTask Operations

```bash
# Create a scheduled task
kubectl apply -f crontask.yaml

# List scheduled tasks
kubectl get crontasks -n kubetask-system

# Watch scheduled task status
kubectl get crontask daily-report -n kubetask-system -w

# Check scheduled task details
kubectl get crontask daily-report -o yaml

# Suspend a scheduled task
kubectl patch crontask daily-report -p '{"spec":{"suspend":true}}' --type=merge

# Resume a scheduled task
kubectl patch crontask daily-report -p '{"spec":{"suspend":false}}' --type=merge

# View child tasks created by CronTask
kubectl get tasks -l kubetask.io/crontask=daily-report -n kubetask-system

# Delete scheduled task
kubectl delete crontask daily-report -n kubetask-system
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

- **Core CRDs**: Task, CronTask, and Agent
- **Clear separation**: WHAT (Task) vs WHEN (CronTask) vs HOW (Agent)
- **Kubernetes-native batch**: Use Helm/Kustomize for multiple Tasks
- **Follows K8s patterns**: CronTask mirrors CronJob behavior

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
- **CronTask** - scheduled/recurring task execution (creates Tasks on cron schedule)
- **Agent** - stable, project-independent configuration
- **KubeTaskConfig** - system-level settings (TTL, lifecycle)

**Context Types**:
- `FilePath` + `Inline` - single file with inline content
- `FilePath` + `ConfigMapKeyRef` - single file from ConfigMap key
- `DirPath` + `ConfigMapRef` - directory with all ConfigMap keys as files

**Task Lifecycle**:
- TTL-based automatic cleanup (default: 7 days)
- Human-in-the-loop debugging support
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
**Date**: 2025-12-10
**Version**: v3.1
**Maintainer**: KubeTask Team
