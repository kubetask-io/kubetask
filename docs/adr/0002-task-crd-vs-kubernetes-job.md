# ADR 0002: Task CRD vs Kubernetes Job

## Status

Accepted

## Context

When presenting KubeTask, two important design questions were raised:

1. **Why use a declarative API for Task?** Task appears to be a one-off execution rather than a "desired state" that should be maintained. Traditional Kubernetes declarative resources (like Deployment) describe a state that controllers continuously reconcile toward. Task, however, represents a single execution - once completed, there's nothing to maintain.

2. **Why not use Kubernetes Jobs directly?** Jobs are the native Kubernetes primitive for running tasks to completion. Why introduce an additional abstraction layer?

These are fundamental questions about the design philosophy of KubeTask that deserve clear documentation.

## Decision

We chose to implement **Task as a domain-specific Custom Resource** that internally creates and manages Kubernetes Jobs. This decision was made for the following reasons:

### 1. Declarative API Semantics for One-off Workloads

The concern that "Task doesn't represent a desired state" reflects a common misconception about Kubernetes declarative APIs. In fact, **Kubernetes Job already follows this pattern**:

- A Job's "desired state" is not "run forever" but rather "run to completion"
- The Job controller reconciles toward "N successful completions"
- Once complete, the Job remains as a record but no further reconciliation occurs

Task follows the same semantic model:

```
Desired State: Task completed successfully
State Machine: Pending → Running → Completed | Failed
```

The value of declarative APIs extends beyond just "maintaining state":

| Benefit | Description |
|---------|-------------|
| **Idempotency** | Applying the same Task YAML multiple times is safe |
| **Auditability** | `kubectl get tasks` shows all task executions and their status |
| **GitOps Compatibility** | Tasks can be version-controlled and applied via ArgoCD/Flux |
| **Garbage Collection** | Owner References enable automatic cleanup |
| **Status Tracking** | Standard `.status` field provides execution state |
| **Event History** | Kubernetes Events record task lifecycle |

### 2. Domain-Specific Abstraction Over Jobs

Task provides significant value beyond what raw Jobs offer:

#### Context Aggregation System

Task's context system handles the complexity of assembling AI agent inputs:

```yaml
spec:
  contexts:
    - type: File
      file:
        filePath: /workspace/task.md
        source:
          inline: |
            Update all dependencies to latest versions.
    - type: File
      file:
        filePath: /workspace/task.md  # Same path - will be aggregated!
        source:
          configMapKeyRef:
            name: guidelines
            key: pr-workflow.md
    - type: Repository
      repository:
        org: myorg
        repo: service-a
```

The Task controller automatically:
- Resolves content from multiple sources (inline, ConfigMap, Secret, RemoteFile)
- Aggregates multiple contexts with the same `filePath` into a single file
- Wraps aggregated content in XML tags for clarity
- Creates ConfigMaps and configures Volume mounts

With raw Jobs, users would need to manually:
- Create ConfigMaps for each content source
- Configure multiple Volume and VolumeMount entries
- Handle content aggregation logic themselves

#### Agent Reuse

Agent centralizes execution environment configuration:

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: default
spec:
  agentImage: quay.io/myorg/claude-agent:v2.0
  toolsImage: quay.io/myorg/dev-tools:latest
  credentials:
    - secretRef: {name: github-token}
      env: {name: GITHUB_TOKEN}
  scheduling:
    nodeSelector:
      workload-type: ai-agent
```

Multiple Tasks share this configuration without repetition. With raw Jobs, each Job would need complete environment specification.

#### BatchRun Integration

The Task abstraction enables batch operations:

```
Batch (WHAT + WHERE template)
    ↓ instantiate
BatchRun (execution instance)
    ↓ creates
Task[0], Task[1], Task[2], ... (one per variableContext)
    ↓ creates
Job[0], Job[1], Job[2], ...
```

BatchRun creates Task CRs, not Jobs directly. This means:
- Standalone Task and batch-created Task use the same abstraction
- No code duplication between single and batch execution paths
- Consistent status tracking and lifecycle management

### 3. Comparison Summary

| Aspect | Raw Kubernetes Job | Task CRD |
|--------|-------------------|----------|
| **Abstraction Level** | Generic container execution | AI-task-specific semantics |
| **Context Handling** | Manual ConfigMap/Volume setup | Automatic aggregation from multiple sources |
| **Environment Config** | Per-Job specification | Agent reference (reusable) |
| **Batch Execution** | External orchestration required | Native BatchRun integration |
| **Extensibility** | Modify Job templates | Add new Context types |
| **API Semantics** | Container/Pod focused | Task/Workflow focused |
| **Learning Curve** | Requires K8s Volume knowledge | Domain-focused API |

### 4. Alternative Considered: Direct Job Creation

We considered having BatchRun create Jobs directly without the Task intermediary:

**Pros:**
- One less abstraction layer
- Slightly simpler codebase

**Cons:**
- No standalone single-task execution API
- Would need separate logic for single vs batch execution
- Users wanting simple one-off tasks would need to use Batch+BatchRun
- Harder to track individual task status in batch scenarios

We rejected this approach because it would complicate the user experience for simple use cases.

## Consequences

### Positive

- **Clear Domain Model**: Users think in terms of "Tasks" not "Jobs with specific configurations"
- **Reduced Boilerplate**: Context aggregation eliminates manual ConfigMap/Volume setup
- **Consistent API**: Same Task abstraction works standalone and in batches
- **Separation of Concerns**: WHAT (Batch) + WHERE (Repository) + HOW (Agent)
- **GitOps Ready**: Declarative resources work naturally with GitOps tools
- **Observability**: `kubectl get tasks` provides AI-task-centric view

### Negative

- **Additional Abstraction**: One more CRD to understand beyond Jobs
- **Indirect Debugging**: Must trace Task → Job → Pod for troubleshooting
- **Learning Curve**: Users must learn Task API even if familiar with Jobs

### Mitigation

- Clear documentation explaining the Task → Job relationship
- Task status includes `jobName` for easy correlation
- Debug commands documented: `kubectl describe task X` → `kubectl logs job/Y`

## References

- [Kubernetes Jobs Documentation](https://kubernetes.io/docs/concepts/workloads/controllers/job/)
- [Operator Pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
- [KubeTask Architecture](../architecture.md)
