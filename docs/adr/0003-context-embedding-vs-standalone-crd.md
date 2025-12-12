# ADR 0003: Context Embedding vs Standalone CRD

## Status

Superseded by [ADR 0004: Context as Standalone CRD](0004-context-as-standalone-crd.md)

## Context

As KubeTask evolves, a design question arose: should `Context` be extracted as a standalone CRD instead of being embedded within Task?

The argument for a standalone Context CRD:
- Contexts could be reused across multiple Tasks
- Independent lifecycle management
- Separate RBAC permissions
- Better GitOps tracking

However, this needs to be evaluated against the overall design philosophy of KubeTask.

## Decision

We decided to **keep Context embedded within Task** and focus on **Agent as the primary reusable abstraction**.

### Design Model

```
Task ──agentRef──► Agent (standalone CRD, reusable)
  │
  └── contexts[] (embedded, task-specific)
```

| Resource | Role | Lifecycle | CRD Type |
|----------|------|-----------|----------|
| **Agent** | Execution config (HOW) | Long-lived, shared | Standalone |
| **Task** | Task definition (WHAT) | One-off execution | Standalone |
| **Context** | Input data (INPUT) | Task-specific | Embedded |

### Rationale

1. **Agent is the core abstraction**
   - Users/platforms define reusable Agents (claude-coder, gemini-reviewer, etc.)
   - Tasks reference Agents via `agentRef`
   - Agent centralizes: image, credentials, scheduling, service account

2. **Context is inherently task-specific**
   - Each Task has unique inputs (code to review, PR diff, etc.)
   - Context content varies per execution
   - No strong use case for cross-Task Context sharing

3. **Existing reuse mechanisms are sufficient**
   - `Agent.defaultContexts`: Base contexts applied to all Tasks using the Agent
   - `ConfigMapRef`: Reference shared ConfigMaps for common files
   - Context merging: Task contexts override/extend Agent defaults

4. **Follows Kubernetes conventions**
   - Similar to `Pod.spec.volumes` - embedded, not a standalone CRD
   - Similar to `Job.spec.template` - embedded PodTemplateSpec
   - Keeps API simple: only 2 CRDs (Task, Agent)

5. **UI-friendly design**
   - User selects an Agent from dropdown (pre-configured)
   - User defines task-specific Contexts in form
   - Clean separation: pick HOW (Agent), define INPUT (Contexts)

## Consequences

### Positive

- **Simple API**: Only 2 CRDs to learn and manage
- **Self-contained Tasks**: Single YAML contains all task information
- **Clear separation**: Agent (reusable HOW) vs Context (task-specific INPUT)
- **Kubernetes-native**: Follows established patterns (Pod.volumes, Job.template)
- **UI-friendly**: Natural workflow of selecting Agent + defining Contexts

### Negative

- **No cross-Task Context references**: Cannot reference a "Context CR" from multiple Tasks
- **Duplication possible**: Similar Contexts may be repeated across Tasks

### Mitigation

- Use `Agent.defaultContexts` for common base contexts
- Use `ConfigMapRef` to reference shared content
- Use Helm/Kustomize templates for batch creation with shared context patterns

## References

- [ADR 0002: Task CRD vs Kubernetes Job](0002-task-crd-vs-kubernetes-job.md)
- [KubeTask Architecture](../architecture.md)
