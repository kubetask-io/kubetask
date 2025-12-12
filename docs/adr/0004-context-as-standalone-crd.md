# ADR 0004: Context as Standalone CRD

## Status

Accepted

## Context

ADR 0003 decided to keep Context embedded within Task, reasoning that Agent was the primary reusable abstraction. However, new requirements have emerged:

1. **Context Engineering Trend**: Context Engineering is becoming increasingly important for AI agent effectiveness. The industry is recognizing that well-structured, reusable contexts significantly improve AI agent performance.

2. **Expected Usage Ratio**: We anticipate a 100 Contexts : 10 Agents : N Tasks ratio, indicating Contexts will be the most numerous and frequently managed resources.

3. **Reusability Requirements**: Organizations want to:
   - Share coding standards, security policies, and best practices across multiple Tasks
   - Update context content without modifying Tasks
   - Version control Contexts independently in Git
   - Apply organization-wide contexts consistently

4. **Separation of Concerns**: Context content (WHAT the agent knows) should be separate from mount location (WHERE it's available). The same coding standards document should be mountable at different paths by different consumers.

## Decision

We decided to **promote Context to a standalone CRD** with a simplified API:
- Task and Agent only reference Context CRDs via `ContextMount`
- No inline contexts in Task/Agent (simplicity over flexibility)
- Context CRD supports Inline, ConfigMap, and Git types

### Design Model

```
Task ──agentRef──► Agent (standalone CRD)
  │                   │
  ├── contexts[]      └── contexts[]    (ContextMount, references Context CRD)
  └── description                       (syntactic sugar for /workspace/task.md)
         │
         ▼
      Context (standalone CRD, reusable)
         │
         ├── Inline    (content directly in YAML)
         ├── ConfigMap (reference to ConfigMap)
         └── Git       (content from Git repository)
```

| Resource | Role | Lifecycle | CRD Type |
|----------|------|-----------|----------|
| **Context** | Reusable knowledge (KNOW) | Long-lived, shared | Standalone |
| **Agent** | Execution config (HOW) | Long-lived, shared | Standalone |
| **Task** | Task definition (WHAT) | One-off execution | Standalone |

### Key Design Decisions

#### 1. Context CRD with Multiple Types

Context CRD supports three source types:
- **Inline**: Content directly in YAML
- **ConfigMap**: Reference to a ConfigMap (key or entire ConfigMap)
- **Git**: Content from a Git repository (future)

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Context
metadata:
  name: coding-standards
spec:
  type: Inline
  inline:
    content: |
      # Coding Standards
      - Use descriptive variable names
      - Write unit tests
```

#### 2. No Mount Path in Context

Mount path is defined by the consumer (Task/Agent) via `ContextMount.mountPath`, not in the Context itself. This allows the same Context to be mounted at different paths.

#### 3. Empty MountPath Behavior

When `ContextMount.mountPath` is empty, the context content is appended to `/workspace/task.md` with XML tags:

```xml
<context name="coding-standards" namespace="default" type="Inline">
... content ...
</context>
```

This enables aggregation of multiple contexts into a single file the agent reads.

#### 4. Task.description Syntactic Sugar

Added `Task.description` field that automatically creates `/workspace/task.md`:

```yaml
spec:
  description: "Update all dependencies and create a PR"
```

This is the simplest way to define what the agent should do.

#### 5. Fixed Interface: /workspace/task.md

All agents read task instructions from `/workspace/task.md`. This standardized interface:
- Simplifies agent implementation
- Enables context aggregation
- Provides consistent entry point

#### 6. No Context Status

Context is a pure data resource (like ConfigMap) with no controller reconciliation or status field. This keeps it simple and performant.

#### 7. No Inline Contexts in Task/Agent

Task and Agent only reference Context CRDs via `ContextMount`. This:
- Keeps the API simple and consistent
- Encourages reusability of contexts
- Reduces duplication across Tasks

### Context Priority

When resolving contexts, lower priority content appears first in task.md:

1. Agent.contexts (lowest priority, organization-wide defaults)
2. Task.contexts (task-specific contexts)
3. Task.description (highest priority, becomes start of task.md)

## Consequences

### Positive

- **Reusability**: Share contexts across multiple Tasks and Agents
- **Independent Lifecycle**: Update context without modifying Tasks
- **Version Control**: Track context changes in Git as standalone resources
- **Separation of Concerns**: Content definition vs mount location
- **Simple API**: Task and Agent have minimal context-related fields
- **Context Engineering**: First-class support for the growing importance of contexts
- **Scalability**: Efficiently manage 100+ contexts in an organization

### Negative

- **Additional CRD**: One more resource type to learn and manage
- **No Inline Option**: Cannot define one-off contexts directly in Task
- **Reference Management**: Need to create Context CRD even for simple cases

### Mitigations

- `Task.description` provides simple UX for the most common case
- Context CRD with `type: Inline` is straightforward for simple content
- Clear documentation and examples

## Examples

### Simple Task with Description

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: update-deps
spec:
  description: "Update all dependencies and create a PR"
  agentRef: gemini
```

### Reusable Context with Inline Content

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Context
metadata:
  name: coding-standards
spec:
  type: Inline
  inline:
    content: |
      # Coding Standards
      - Use descriptive variable names
      - Write unit tests for all functions
      - Follow Go conventions
---
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: code-review
spec:
  description: "Review the PR for coding standard violations"
  contexts:
    - name: coding-standards
      mountPath: /workspace/guides/standards.md
  agentRef: claude
```

### Context from ConfigMap

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

### Context Aggregation (Empty MountPath)

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: comprehensive-review
spec:
  description: "Perform comprehensive code review"
  contexts:
    - name: coding-standards    # Appended to task.md with XML tags
    - name: security-policy     # Appended to task.md with XML tags
  agentRef: claude
```

Results in `/workspace/task.md`:
```
Perform comprehensive code review

<context name="coding-standards" namespace="default" type="Inline">
... coding standards content ...
</context>

<context name="security-policy" namespace="default" type="ConfigMap">
... security policy content ...
</context>
```

### Agent with Default Contexts

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: claude-coder
spec:
  agentImage: quay.io/myorg/claude-agent:v1.0
  serviceAccountName: kubetask-agent
  contexts:
    - name: org-coding-standards  # Applied to all tasks using this agent
    - name: org-security-policy   # Applied to all tasks using this agent
```

## Future Extensions

- **ClusterContext**: Cluster-scoped Context for cross-namespace sharing
- **Git Source**: Recursive directory trees from Git repositories
- **MCP Context**: Model Context Protocol integration
- **API Context**: Dynamic context from external APIs

## References

- [ADR 0003: Context Embedding vs Standalone CRD](0003-context-embedding-vs-standalone-crd.md) (superseded)
- [KubeTask Architecture](../architecture.md)
