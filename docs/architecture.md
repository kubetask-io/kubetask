# KubeTask Architecture & API Design

## Table of Contents

1. [System Overview](#system-overview)
2. [API Design](#api-design)
3. [System Architecture](#system-architecture)
4. [Custom Resource Definitions](#custom-resource-definitions)
5. [Agent Template Discovery](#agent-template-discovery)
6. [Complete Examples](#complete-examples)
7. [kubectl Usage](#kubectl-usage)
8. [Migration Guide](#migration-guide)

---

## System Overview

KubeTask is a Kubernetes-native system that executes AI-powered tasks across multiple repositories using Custom Resources (CRs) and the Operator pattern.

### Core Goals

- Use Kubernetes CRDs to define Batch and Task resources
- Use Controller pattern to manage resource lifecycle
- Execute tasks as Kubernetes Jobs
- No external databases or message queues required
- Seamless integration with Kubernetes ecosystem

### Key Advantages

✅ **Simplified Architecture**: No PostgreSQL, Redis - reduced component dependencies
✅ **Native Integration**: Works seamlessly with Tekton, Argo, Flux and other K8s tools
✅ **Declarative Management**: Use K8s resource definitions, supports GitOps
✅ **Infrastructure Reuse**: Logs, monitoring, auth/authz all leverage K8s capabilities
✅ **Simplified Operations**: Manage with standard K8s tools (kubectl, dashboard)

---

## API Design

### Complete Resource Overview

| Resource | Purpose | Stability |
|----------|---------|-----------|
| **Batch** | Task batch template (WHAT + WHERE) | Stable - semantic name |
| **BatchRun** | Batch execution instance | Stable - follows Batch |
| **Task** | Single task execution (simplified API) | Stable - semantic name |
| **Agent** | Workspace environment config (HOW) | **Stable - independent of project name** |

### Key Design Decisions

#### 1. Batch (not Bundle)

**Rationale**: "Batch" better expresses batch processing concept and aligns with K8s `batch/v1`.

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Batch
```

#### 2. Agent (not KubeTaskConfig)

**Rationale**:
- ✅ **Stable**: Independent of project name - won't change even if project renames
- ✅ **Semantic**: Reflects architecture philosophy: "Workspace = AI + permissions + tools"
- ✅ **Clear**: Configures the workspace environment for task execution

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Agent
```

#### 3. AgentImage (simplified from AgentTemplateRef)

**Rationale**: Simpler API - users only need to specify the agent container image, not a full Job template.

```yaml
spec:
  agentImage: quay.io/zhaoxue/kubetask-agent:latest
```

#### 4. variableContexts (not targets/variants/contextSets)

**Rationale**: Highlights constant/variable dichotomy perfectly.

```yaml
spec:
  commonContext: [...]      # Constant
  variableContexts: [...]   # Variable
```

### Resource Hierarchy

```
Batch (template)
├── BatchSpec
│   ├── commonContext: []Context
│   ├── variableContexts: [][]Context
│   └── agentRef: string
│
BatchRun (execution)
├── BatchRunSpec
│   ├── batchRef: string
│   └── batchSpec: *BatchSpec (inline, includes agentRef)
└── BatchRunStatus
    ├── phase: BatchRunPhase
    ├── progress: ProgressStatus
    └── tasks: []TaskStatus

Task (single task)
├── TaskSpec
│   ├── contexts: []Context
│   └── agentRef: string
└── TaskExecutionStatus
    ├── phase: TaskPhase
    ├── jobName: string
    ├── startTime: Time
    └── completionTime: Time

Agent (environment)
└── AgentSpec
    └── agentImage: string
```

### Complete Type Definitions

```go
// Core resources
type Batch struct {
    Spec BatchSpec
}

type BatchSpec struct {
    CommonContext      []Context
    VariableContexts   [][]Context
    AgentRef string  // Reference to Agent
}

type BatchRun struct {
    Spec   BatchRunSpec
    Status BatchRunStatus
}

type BatchRunSpec struct {
    BatchRef  string
    BatchSpec *BatchSpec  // Inline batch (includes AgentRef)
}

type Task struct {
    Spec   TaskSpec
    Status TaskExecutionStatus
}

type TaskSpec struct {
    Contexts           []Context
    AgentRef string  // Reference to Agent
}

type TaskExecutionStatus struct {
    Phase          TaskPhase
    JobName        string
    StartTime      *metav1.Time
    CompletionTime *metav1.Time
}

type Agent struct {
    Spec AgentSpec
}

type AgentSpec struct {
    AgentImage string  // Container image for the agent
}

// Context system
type Context struct {
    Type       ContextType
    File       *FileContext
    Repository *RepositoryContext
}

type ContextType string
const (
    ContextTypeFile       ContextType = "File"
    ContextTypeRepository ContextType = "Repository"
)
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
│  - Watch Batch & BatchRun CRs                              │
│  - Reconcile loop                                           │
│  - Create Kubernetes Jobs for tasks                         │
│  - Update CR status fields                                  │
│  - Handle retries and failures                              │
└─────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                   Kubernetes Jobs/Pods                      │
│  - Each task runs as a separate Job/Pod                    │
│  - Execute task using agent scripts                         │
│  - Git worktree management                                  │
│  - AI agent invocation                                      │
│  - Update parent CR status                                  │
└─────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                      Storage Layer                          │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ etcd (Kubernetes Backend)                            │   │
│  │  - Batch CRs                                         │   │
│  │  - BatchRun CRs                                      │   │
│  │  - CR status (execution state, results)              │   │
│  └──────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ PersistentVolume (PVC)                               │   │
│  │  - /workspace (bare repos)                           │   │
│  │  - Pod logs (managed by Kubernetes)                  │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

---

## Custom Resource Definitions

### Design Principles

KubeTask follows **separation of concerns**:

- **Batch**: Defines task content (WHAT) and repository scope (WHERE)
- **Agent**: Defines execution config (HOW) - Agent template reference
- **BatchRun**: Execution instance, can override Agent settings

**Benefits**:
1. **Simple Batch**: Only focuses on task definition, easy to create and maintain
2. **Centralized Config**: All execution config managed in Agent
3. **Flexible Override**: BatchRun can override default config as needed
4. **Stability**: Agent name independent of project name, won't change if project renames

### Batch (Task Definition)

Batch defines the content and targets of batch processing tasks.

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Batch
metadata:
  name: update-dependencies
  namespace: kubetask-system
spec:
  # Common context - shared by all tasks
  commonContext:
    - type: File
      file:
        name: task.md
        source:
          inline: |
            Update dependencies to latest versions.
            Run tests and create PR.

    - type: File
      file:
        name: guide.md
        source:
          configMapKeyRef:
            name: workflow-guides
            key: standard-pr-workflow.md

  # Variable context - different per task
  variableContexts:
    # Task 1: service-a + its config
    - - type: Repository
        repository:
          org: myorg
          repo: service-a
          branch: main

      - type: File
        file:
          name: config.json
          source:
            configMapKeyRef:
              name: dep-configs
              key: service-a.json

    # Task 2: service-b + its config + credentials
    - - type: Repository
        repository:
          org: myorg
          repo: service-b
          branch: main

      - type: File
        file:
          name: config.json
          source:
            configMapKeyRef:
              name: dep-configs
              key: service-b.json

      - type: File
        file:
          name: credentials.json
          source:
            secretKeyRef:
              name: private-credentials
              key: service-b-creds.json

    # Task 3: service-c on develop branch
    - - type: Repository
        repository:
          org: myorg
          repo: service-c
          branch: develop

      - type: File
        file:
          name: config.json
          source:
            configMapKeyRef:
              name: dep-configs
              key: service-c.json
```

**Field Description:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.commonContext` | []Context | Yes | Common context, shared by all tasks |
| `spec.variableContexts` | [][]Context | Yes | Variable context, each element generates one task |
| `spec.agentRef` | String | No | Reference to Agent (default: "default") |

**Task Generation Formula:**

```
Task[i] = commonContext + variableContexts[i]
Total Tasks = len(variableContexts)
```

**Example:**
```
commonContext = [task.md, guide.md]
variableContexts = [
  [service-a:main, service-a.json],
  [service-b:main, service-b.json, creds.json],
  [service-c:develop, service-c.json]
]

Generated Tasks:
Task 1 = [task.md, guide.md, service-a:main, service-a.json]
Task 2 = [task.md, guide.md, service-b:main, service-b.json, creds.json]
Task 3 = [task.md, guide.md, service-c:develop, service-c.json]

Total: 3 tasks
```

**Context Types:**

1. **File Context**:
```yaml
type: File
file:
  name: task.md
  source:
    inline: "Task description"
    # OR
    configMapKeyRef:
      name: configs
      key: task.md
    # OR
    secretKeyRef:
      name: secrets
      key: credentials.json
```

2. **Repository Context**:
```yaml
type: Repository
repository:
  org: myorg
  repo: service-a
  branch: main
```

### BatchRun (Execution Instance)

BatchRun is a concrete execution instance of a Batch. Each execution creates a new Run.

```yaml
apiVersion: kubetask.io/v1alpha1
kind: BatchRun
metadata:
  name: update-dependencies-run-001
  namespace: kubetask-system
  labels:
    team: platform
    jira: PROJ-1234
spec:
  # Method 1: Reference existing Batch (recommended)
  batchRef: update-dependencies

  # Method 2: Inline Batch definition (optional, choose one)
  # batchSpec:
  #   commonContext: [...]
  #   variableContexts: [...]

status:
  # Execution phase (BatchRunPhase enum)
  phase: Running  # Pending|Running|Succeeded|Failed

  # Start and end times
  startTime: "2025-01-18T10:00:00Z"
  completionTime: "2025-01-18T10:15:00Z"

  # Progress statistics
  progress:
    total: 2
    pending: 0
    running: 1
    completed: 1
    failed: 0

  # Task list
  tasks:
    - contexts:
        - type: Repository
          repository:
            org: myorg
            repo: service-a
            branch: main
      status: Succeeded  # TaskPhase enum: Pending|Running|Succeeded|Failed
      jobName: update-dependencies-run-001-task-0
      startTime: "2025-01-18T10:01:00Z"
      completionTime: "2025-01-18T10:05:00Z"

    - contexts:
        - type: Repository
          repository:
            org: myorg
            repo: service-b
            branch: main
      status: Running
      jobName: update-dependencies-run-001-task-1
      startTime: "2025-01-18T10:02:00Z"
```

**Status Field Description:**

| Field | Type | Description |
|-------|------|-------------|
| `status.phase` | BatchRunPhase | Execution phase: Pending\|Running\|Succeeded\|Failed |
| `status.startTime` | Timestamp | Start time |
| `status.completionTime` | Timestamp | End time |
| `status.progress` | Object | Progress statistics |
| `status.tasks` | []TaskStatus | Task details list |

### Task (Single Task Execution)

Task is a simplified API for users who want to execute a single task without creating a Batch. Unlike BatchRun which manages multiple tasks, Task is for simple one-off executions.

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: update-service-a
  namespace: kubetask-system
  labels:
    team: platform
spec:
  # Contexts defines what this task operates on
  # This is a simple list of contexts (no common/variable separation)
  contexts:
    # File context - task description
    - type: File
      file:
        name: task.md
        source:
          inline: |
            Update dependencies to latest versions.
            Run tests and create PR.

    # File context - workflow guide
    - type: File
      file:
        name: guide.md
        source:
          configMapKeyRef:
            name: workflow-guides
            key: standard-pr-workflow.md

    # Repository context
    - type: Repository
      repository:
        org: myorg
        repo: service-a
        branch: main

    # Config file from ConfigMap
    - type: File
      file:
        name: config.json
        source:
          configMapKeyRef:
            name: dep-configs
            key: service-a.json

status:
  # Execution phase (TaskPhase enum)
  phase: Running  # Pending|Running|Succeeded|Failed

  # Kubernetes Job name
  jobName: update-service-a-xyz123

  # Start and end times
  startTime: "2025-01-18T10:00:00Z"
  completionTime: "2025-01-18T10:05:00Z"
```

**Field Description:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.contexts` | []Context | Yes | List of contexts (files, repositories, etc.) |

**Status Field Description:**

| Field | Type | Description |
|-------|------|-------------|
| `status.phase` | TaskPhase | Execution phase: Pending\|Running\|Succeeded\|Failed |
| `status.jobName` | String | Kubernetes Job name |
| `status.startTime` | Timestamp | Start time |
| `status.completionTime` | Timestamp | End time |

**When to Use Task vs Batch:**

- **Use Task**: For simple one-off executions on a single repository
- **Use Batch + BatchRun**: For executing the same task across multiple repositories

**Example Comparison:**

```yaml
# Using Task (simple, single execution)
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: update-service-a
spec:
  contexts:
    - type: File
      file: {name: task.md, source: {inline: "Update deps"}}
    - type: Repository
      repository: {org: myorg, repo: service-a, branch: main}

# Using Batch (template for multiple executions)
apiVersion: kubetask.io/v1alpha1
kind: Batch
metadata:
  name: update-dependencies
spec:
  commonContext:
    - type: File
      file: {name: task.md, source: {inline: "Update deps"}}
  variableContexts:
    - [{type: Repository, repository: {org: myorg, repo: service-a, branch: main}}]
    - [{type: Repository, repository: {org: myorg, repo: service-b, branch: main}}]
    - [{type: Repository, repository: {org: myorg, repo: service-c, branch: main}}]
```

### Agent (Execution Configuration)

Agent defines the agent container image for task execution. The controller generates Jobs using this image.

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: default  # Convention: "default" is used when no agentRef is specified
  namespace: kubetask-system
spec:
  # Agent container image
  # If not specified, defaults to "quay.io/zhaoxue/kubetask-agent:latest"
  agentImage: quay.io/myorg/custom-agent:v1.0
```

**Field Description:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.agentImage` | String | No | Agent container image (default: "quay.io/zhaoxue/kubetask-agent:latest") |

**Design Points:**
- Simplified API: Only specify the agent image, controller handles Job creation
- Controller generates Jobs with consistent structure (env vars, labels, owner references)
- Name independent of project name, won't change if project renames
- Tasks and BatchRuns reference Agent via `agentRef` field

---

## Agent Image Discovery

### Design Philosophy

**Core Idea**: KubeTask controller generates Jobs internally, users only need to specify the agent container image.

**User Philosophy**: Work is not produced by people (AI agents) alone, but by **workspace environments**. Workspace = AI intelligence + permissions + tools.

### Discovery Priority

Controller determines the agent image in this priority order:

1. **Agent.spec.agentImage** (from referenced Agent)
2. **Built-in default** (fallback) - `quay.io/zhaoxue/kubetask-agent:latest`

### How It Works

The controller:
1. Looks up the Agent referenced by `agentRef` (defaults to "default")
2. Uses the `agentImage` from Agent if specified
3. Falls back to built-in default image if no Agent or agentImage found
4. Generates a Job with consistent structure including:
   - Labels for tracking (`kubetask.io/task`)
   - Environment variables (`TASK_NAME`, `TASK_NAMESPACE`)
   - Owner references for garbage collection
   - ServiceAccount `kubetask-agent`

### Usage Scenarios

#### Scenario 1: Standard Usage (90% of cases)

**User Operations**:
```bash
# 1. Create namespace and workspace resources
kubectl create namespace team-platform
kubectl apply -f workspace-setup.yaml  # PVC, Secrets, ServiceAccount

# 2. Create default Agent (optional - uses built-in default if not created)
kubectl apply -f - <<EOF
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: default
  namespace: team-platform
spec:
  agentImage: quay.io/myorg/claude-agent:v1.0
EOF

# 3. Create and run Task (uses "default" Agent automatically)
kubectl apply -f - <<EOF
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: update-service-a
  namespace: team-platform
spec:
  contexts:
    - type: File
      file:
        name: task.md
        source:
          inline: "Update dependencies"
    - type: Repository
      repository:
        org: myorg
        repo: service-a
        branch: main
EOF
```

#### Scenario 2: Multiple Workspace Configurations

**User Operations**:
```bash
# 1. Create different Agents for different AI agents
kubectl apply -f - <<EOF
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: claude-workspace
  namespace: team-platform
spec:
  agentImage: quay.io/myorg/claude-agent:v1.0
---
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: gemini-workspace
  namespace: team-platform
spec:
  agentImage: quay.io/myorg/gemini-agent:v1.0
EOF

# 2. Task using Claude
kubectl apply -f - <<EOF
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: task-with-claude
  namespace: team-platform
spec:
  agentRef: claude-workspace
  contexts:
    - type: Repository
      repository: {org: myorg, repo: service-a, branch: main}
EOF

# 3. Task using Gemini
kubectl apply -f - <<EOF
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: task-with-gemini
  namespace: team-platform
spec:
  agentRef: gemini-workspace
  contexts:
    - type: Repository
      repository: {org: myorg, repo: service-b, branch: main}
EOF
```

#### Scenario 3: Batch with Custom Workspace

```bash
# Create Batch with specific Agent
kubectl apply -f - <<EOF
apiVersion: kubetask.io/v1alpha1
kind: Batch
metadata:
  name: update-with-gemini
  namespace: team-platform
spec:
  agentRef: gemini-workspace  # Use Gemini for this batch
  commonContext:
    - type: File
      file:
        name: task.md
        source:
          inline: "Update dependencies"
  variableContexts:
    - [{type: Repository, repository: {org: myorg, repo: service-a, branch: main}}]
---
apiVersion: kubetask.io/v1alpha1
kind: BatchRun
metadata:
  name: update-deps-001
  namespace: team-platform
spec:
  batchRef: update-with-gemini
EOF
```

### Advantages

✅ **Simple**: Users only specify an image, no need to understand Job template structure
✅ **Consistent**: Controller generates Jobs with consistent labels, env vars, and owner references
✅ **Flexible**: Different Agents for different AI agents
✅ **Good Performance**: Direct Get() call for Agent lookup
✅ **Clear**: Simple error messages when Agent not found
✅ **Stability**: Agent name independent of project name

---

## Complete Examples

### 1. Define Workspace (Environment)

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: default  # Convention: "default" is used when no agentRef is specified
  namespace: kubetask-system
spec:
  agentImage: quay.io/myorg/claude-agent:v1.0
```

### 2. Define Batch (What to do)

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Batch
metadata:
  name: update-dependencies
  namespace: kubetask-system
spec:
  # Constant - shared by all tasks
  commonContext:
    - type: File
      file:
        name: task.md
        source:
          inline: |
            Update dependencies to latest versions.
            Run tests and create PR.

    - type: File
      file:
        name: guide.md
        source:
          configMapKeyRef:
            name: workflow-guides
            key: standard-pr-workflow.md

  # Variable - different per task
  variableContexts:
    # Task 1: service-a + config
    - - type: Repository
        repository:
          org: myorg
          repo: service-a
          branch: main

      - type: File
        file:
          name: config.json
          source:
            configMapKeyRef:
              name: dep-configs
              key: service-a.json

    # Task 2: service-b + config + credentials
    - - type: Repository
        repository:
          org: myorg
          repo: service-b
          branch: main

      - type: File
        file:
          name: config.json
          source:
            configMapKeyRef:
              name: dep-configs
              key: service-b.json

      - type: File
        file:
          name: credentials.json
          source:
            secretKeyRef:
              name: private-credentials
              key: service-b-creds.json
```

### 3. Execute Batch

```yaml
apiVersion: kubetask.io/v1alpha1
kind: BatchRun
metadata:
  name: update-dependencies-run-001
  namespace: kubetask-system
  labels:
    team: platform
    jira: PROJ-1234
spec:
  # Reference Batch (inherits agentRef from Batch)
  batchRef: update-dependencies
```

---

## kubectl Usage

### Single Task Operations

```bash
# Create and run a single task
kubectl apply -f - <<EOF
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: update-service-a
  namespace: kubetask-system
spec:
  contexts:
    - type: File
      file:
        name: task.md
        source:
          inline: "Update dependencies and create PR"
    - type: Repository
      repository:
        org: myorg
        repo: service-a
        branch: main
EOF

# List tasks
kubectl get tasks -n kubetask-system

# Watch task execution
kubectl get task update-service-a -n kubetask-system -w

# Check task status
kubectl get task update-service-a -o yaml

# View task logs
kubectl logs job/$(kubectl get task update-service-a -n kubetask-system -o jsonpath='{.status.jobName}') -n kubetask-system

# Delete completed task
kubectl delete task update-service-a -n kubetask-system
```

### Batch Operations

```bash
# List batches
kubectl get batches -n kubetask-system

# Create a batch
kubectl apply -f my-batch.yaml

# Run a batch
kubectl apply -f - <<EOF
apiVersion: kubetask.io/v1alpha1
kind: BatchRun
metadata:
  generateName: update-deps-
  namespace: kubetask-system
spec:
  batchRef: update-dependencies
EOF

# Watch batch execution
kubectl get batchrun -n kubetask-system -w

# Check batch run status
kubectl get batchrun update-deps-abc123 -o yaml

# View logs for a specific task in batch
kubectl logs job/update-deps-abc123-task-0 -n kubetask-system

# List all batch runs
kubectl get batchruns -n kubetask-system
```

### Workspace Configuration

```bash
# List workspace configs
kubectl get agents -n kubetask-system

# Create workspace config
kubectl apply -f workspace-config.yaml

# View workspace config details
kubectl get agent default-workspace -o yaml
```

---

## Naming Stability Analysis

### Stable Names (Won't Change)

| Name | Reason |
|------|--------|
| **Agent** | Independent of project name; semantic concept |
| **AgentTemplateRef** | Semantic concept; describes what it is |
| **commonContext** | Programming concept; universal |
| **variableContexts** | Programming concept; universal |
| **Context** | Universal abstraction |

### Project-Dependent Names (May Change)

| Name | Current | If Project Renames |
|------|---------|-------------------|
| Namespace | `kubetask-system` | Would change |
| ConfigMap | `kubetask-agent` | Would change |
| Labels | `kubetask.io/*` | Would change |

**Mitigation**: Use convention-based discovery. Agent doesn't hardcode names.

---

## Migration Guide

### Migration from CodeSweep

#### Resource Mapping

| Old (CodeSweep) | New (KubeTask) |
|----------------|----------------|
| `Bundle` | `Batch` |
| `BundleRun` | `BatchRun` |
| `CodeSweepConfig` | `Agent` |
| `bundles.codesweep.io` | `batches.kubetask.io` |
| `bundleruns.codesweep.io` | `batchruns.kubetask.io` |
| `codesweepconfigs.codesweep.io` | `agents.kubetask.io` |

#### Field Mapping

| Old | New |
|-----|-----|
| `spec.repositories` | `spec.variableContexts` (with type: Repository) |
| `spec.context.files` | `spec.commonContext` (with type: File) |
| `spec.bundleRef` | `spec.batchRef` |
| `spec.jobTemplateRef` | `spec.agentRef` |

---

## Convention-Based Discovery

**Agent image discovery order:**

1. **Agent.spec.agentImage** (from referenced Agent)
2. **Built-in default** (fallback: `quay.io/zhaoxue/kubetask-agent:latest`)

**Agent lookup:**
- Task/Batch uses `agentRef` field to reference a Agent
- If not specified, uses Agent named "default" in the same namespace
- If "default" doesn't exist, uses built-in default image

This allows:
- ✅ Explicit Agent per Batch/Task
- ✅ Convention-based default ("default" Agent)
- ✅ Fallback for new users (built-in default image)

---

## Benefits of Final Design

### 1. Semantic Clarity

- **Batch**: Clearly batch processing
- **Agent**: Clearly environment config
- **AgentImage**: Simple container image reference
- **commonContext/variableContexts**: Clearly constant/variable

### 2. Stability

- **Agent**: Won't change even if project renames
- **AgentImage**: Simple, semantic field
- **Core concepts**: Independent of project name

### 3. Flexibility

- Multiple context types (File, Repository, future: API, Database)
- Each task can have different contexts
- Easy to extend

### 4. K8s Alignment

- **Batch**: Aligns with `batch/v1`
- **Agent**: Follows K8s Config pattern
- Convention-based discovery: K8s standard practice

---

## Summary

**Final API**:
- ✅ **Batch** + **BatchRun** - semantic batch processing
- ✅ **Task** - simplified single task execution
- ✅ **Agent** - stable, project-independent
- ✅ **AgentImage** - simple container image configuration
- ✅ **commonContext** + **variableContexts** - clear constant/variable model
- ✅ **agentRef** - reference to Agent from Batch/Task
- ✅ **Context** abstraction - flexible, extensible

**Philosophy**:
- **Stability**: Core concepts independent of project name
- **Semantics**: Names reflect what things are, not project branding
- **Flexibility**: Extensible context system
- **Alignment**: Follows K8s patterns and conventions

**Advantages**:
- ✅ **Simplified Architecture**: No external dependencies
- ✅ **Native Integration**: Works with K8s ecosystem
- ✅ **Declarative Management**: GitOps ready
- ✅ **Infrastructure Reuse**: Leverage K8s capabilities
- ✅ **Simplified Operations**: Standard K8s tools

---

## Related Documentation

- **[ADR 0003: Use Kubernetes-Native Architecture](adr/0003-kubernetes-native-architecture.md)** - Architecture decision record
- **[ADR 0005: Task Abstraction and Context System](adr/0005-task-abstraction-and-context-system.md)** - Task abstraction and context system
- **[FINAL-NAMING-DECISIONS.md](FINAL-NAMING-DECISIONS.md)** - Naming decision records

---

**Status**: ✅ FINAL
**Date**: 2025-12-01
**Version**: v2.0
**Approved**: @zxue
**Maintainer**: KubeTask Team
