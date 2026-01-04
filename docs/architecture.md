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

KubeOpenCode is a Kubernetes-native system that executes AI-powered tasks using Custom Resources (CRs) and the Operator pattern. It provides a simple, declarative way to run AI agents as Kubernetes Jobs, using OpenCode as the primary AI coding tool.

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
- Jobs are created with `backoffLimit: 0` (no Pod retry on failure)
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
│   └── agentRef: string
└── TaskExecutionStatus
    ├── phase: TaskPhase
    ├── jobName: string
    ├── startTime: Time
    ├── completionTime: Time
    └── conditions: []Condition

Agent (execution configuration)
└── AgentSpec
    ├── agentImage: string
    ├── workspaceDir: string         (default: "/workspace")
    ├── command: []string
    ├── contexts: []ContextItem      (inline context definitions)
    ├── credentials: []Credential
    ├── podSpec: *AgentPodSpec
    ├── serviceAccountName: string
    └── maxConcurrentTasks: *int32   (limit concurrent Tasks, nil/0 = unlimited)

KubeOpenCodeConfig (system configuration)
└── KubeOpenCodeConfigSpec
    └── systemImage: *SystemImageConfig       (internal KubeOpenCode components)
        ├── image: string                     (default: DefaultKubeOpenCodeImage)
        └── imagePullPolicy: PullPolicy       (default: IfNotPresent)
```

### Complete Type Definitions

```go
// Task represents a single task execution
type Task struct {
    Spec   TaskSpec
    Status TaskExecutionStatus
}

type TaskSpec struct {
    Description *string       // Syntactic sugar for /workspace/task.md
    Contexts    []ContextItem // Inline context definitions
    AgentRef    string        // Reference to Agent
}

// ContextItem defines inline context content
type ContextItem struct {
    Type      ContextType       // Text, ConfigMap, Git, or Runtime
    MountPath string            // Empty = append to /workspace/task.md (ignored for Runtime)
    FileMode  *int32            // Optional file permission mode (e.g., 0755 for executable)
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
    AgentImage         string
    WorkspaceDir       string           // Working directory (default: "/workspace")
    Command            []string         // Custom entrypoint command
    Contexts           []ContextItem    // Inline context definitions
    Credentials        []Credential
    PodSpec            *AgentPodSpec    // Pod configuration (labels, scheduling, runtime)
    ServiceAccountName string
    MaxConcurrentTasks *int32           // Limit concurrent Tasks (nil/0 = unlimited)
}

// KubeOpenCodeConfig defines system-level configuration
type KubeOpenCodeConfig struct {
    Spec KubeOpenCodeConfigSpec
}

type KubeOpenCodeConfigSpec struct {
    SystemImage *SystemImageConfig // System image for internal components
}

// SystemImageConfig configures the KubeOpenCode system image
type SystemImageConfig struct {
    Image           string            // System image (default: built-in DefaultKubeOpenCodeImage)
    ImagePullPolicy corev1.PullPolicy // Pull policy: Always/Never/IfNotPresent (default: IfNotPresent)
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
│  - Create Kubernetes Jobs for tasks                         │
│  - Update CR status fields                                  │
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
      # Empty mountPath = append to task.md with XML tags

  # Optional: Reference to Agent (defaults to "default")
  agentRef: my-agent

status:
  # Execution phase
  phase: Running  # Pending|Queued|Running|Completed|Failed

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
| `spec.contexts` | []ContextItem | No | Inline context definitions (see below) |
| `spec.agentRef` | String | No | Reference to Agent (default: "default") |

**Status Field Description:**

| Field | Type | Description |
|-------|------|-------------|
| `status.phase` | TaskPhase | Execution phase: Pending\|Queued\|Running\|Completed\|Failed |
| `status.jobName` | String | Kubernetes Job name |
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
      path: .claude/
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

**ContextItem Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | ContextType | Yes | Type of context: Text, ConfigMap, Git, or Runtime |
| `mountPath` | string | No | Where to mount (empty = append to task.md with XML tags) |
| `fileMode` | *int32 | No | File permission mode (e.g., 0755 for executables) |
| `text` | string | When type=Text | Text content |
| `configMap` | ConfigMapContext | When type=ConfigMap | Reference to ConfigMap |
| `git` | GitContext | When type=Git | Content from Git repository |
| `runtime` | RuntimeContext | When type=Runtime | Platform awareness (no fields - content is generated by controller) |

**Important Notes:**

- **Empty MountPath behavior**: When mountPath is empty, content is appended to `/workspace/task.md` with XML tags
- **Runtime context**: Provides KubeOpenCode platform awareness to agents, explaining environment variables, kubectl commands, and system concepts
- **Path resolution**: Relative paths are prefixed with workspaceDir; absolute paths are used as-is

**Context Priority (lowest to highest):**

1. Agent.contexts (array order)
2. Task.contexts (array order)
3. Task.description (becomes start of /workspace/task.md)

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
  # Executor container image (worker that runs OpenCode)
  agentImage: quay.io/kubeopencode/kubeopencode-agent-devbox:latest

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
| `spec.agentImage` | String | No | Agent container image |
| `spec.workspaceDir` | String | No | Working directory (default: "/workspace") |
| `spec.command` | []String | No | Custom entrypoint command |
| `spec.contexts` | []ContextItem | No | Inline contexts (applied to all tasks) |
| `spec.credentials` | []Credential | No | Secrets as env vars or file mounts |
| `spec.podSpec` | *AgentPodSpec | No | Advanced Pod configuration (labels, scheduling, runtimeClass) |
| `spec.maxConcurrentTasks` | *int32 | No | Limit concurrent Tasks (nil/0 = unlimited) |
| `spec.serviceAccountName` | String | Yes | ServiceAccount for agent pods |

**Task Stop:**

Running Tasks can be stopped by setting the `kubeopencode.io/stop=true` annotation:

```bash
kubectl annotate task my-task kubeopencode.io/stop=true
```

When this annotation is detected:
- The controller suspends the Job (sets `spec.suspend=true`)
- Kubernetes sends SIGTERM to all running Pods, triggering graceful shutdown
- Job and Pod are preserved (not deleted), so **logs remain accessible**
- Task status is set to `Completed` with a `Stopped` condition

---

## Agent Configuration

### Agent Image Discovery

KubeOpenCode uses a **two-container pattern** for AI task execution:

1. **Init Container** (OpenCode image): Copies OpenCode binary to `/tools` shared volume
2. **Worker Container** (Executor image): Uses `/tools/opencode` to run AI tasks

The executor image is discovered in this priority order:

1. **Agent.spec.agentImage** (from referenced Agent)
2. **Built-in default** (fallback) - `quay.io/kubeopencode/kubeopencode-agent-devbox:latest`

### How It Works

The controller:
1. Looks up the Agent referenced by `agentRef` (defaults to "default")
2. Uses the `agentImage` from Agent as the executor image
3. Falls back to built-in default executor image if no Agent or agentImage found
4. Generates a Job with:
   - Init container that copies OpenCode binary to `/tools`
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
  agentImage: quay.io/kubeopencode/kubeopencode-agent-devbox:latest
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

---

## System Configuration

### KubeOpenCodeConfig (System-level Configuration)

KubeOpenCodeConfig provides cluster or namespace-level settings for container image configuration.

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
```

**Field Description:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.systemImage.image` | string | No | System image for internal components (default: built-in DefaultKubeOpenCodeImage) |
| `spec.systemImage.imagePullPolicy` | string | No | Pull policy for system containers: Always, Never, IfNotPresent (default: IfNotPresent) |

**Image Pull Policy:**

Setting `imagePullPolicy: Always` is recommended when:
- Using `:latest` tags in development/staging environments
- Nodes may have cached old images that differ from registry versions
- Frequent image updates are expected

The `systemImage` configuration affects all internal KubeOpenCode containers:
- `git-init`: Clones Git repositories for Context
- `context-init`: Copies ConfigMap content to writable workspace

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
  # Executor image (worker container)
  agentImage: quay.io/kubeopencode/kubeopencode-agent-devbox:latest
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
  agentRef: claude
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
kubectl logs job/$(kubectl get task update-service-a -o jsonpath='{.status.jobName}') -n kubeopencode-system

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
- **KubeOpenCodeConfig** - system-level settings (systemImage)

**Context Types** (via ContextItem):
- `Text` - Content directly in YAML
- `ConfigMap` - Content from ConfigMap (single key or all keys as directory)
- `Git` - Content from Git repository with branch/tag/commit support
- `Runtime` - KubeOpenCode platform awareness (environment variables, kubectl commands, system concepts)

**Task Lifecycle**:
- No retry on failure (AI tasks are non-idempotent)
- User-initiated stop via `kubeopencode.io/stop=true` annotation (graceful, logs preserved)
- OwnerReference cascade deletion

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
**Date**: 2026-01-04
**Version**: v6.0 (OpenCode Init Container + Executor pattern)
**Maintainer**: KubeOpenCode Team
