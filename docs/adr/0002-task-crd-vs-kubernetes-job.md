# ADR 0002: Task CRD vs Kubernetes Job

## Status

Superseded by [ADR 0009](0009-job-to-pod-migration.md) (2026-01-05)

Originally Accepted (2025-12-10)

## Context

When presenting KubeOpenCode, two important design questions were raised:

1. **Why use a declarative API for Task?** Task appears to be a one-off execution rather than a "desired state" that should be maintained. Traditional Kubernetes declarative resources (like Deployment) describe a state that controllers continuously reconcile toward. Task, however, represents a single execution - once completed, there's nothing to maintain.

2. **Why not use Kubernetes Jobs directly?** Jobs are the native Kubernetes primitive for running tasks to completion. Why introduce an additional abstraction layer?

These are fundamental questions about the design philosophy of KubeOpenCode that deserve clear documentation.

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
    - type: File
      file:
        dirPath: /workspace/configs  # Directory mount
        source:
          configMapRef:
            name: project-configs
```

The Task controller automatically:
- Resolves content from multiple sources (inline, ConfigMap)
- Aggregates multiple contexts with the same `filePath` into a single file
- Wraps aggregated content in XML tags for clarity
- Mounts ConfigMaps as directories with `dirPath` + `configMapRef`
- Creates ConfigMaps and configures Volume mounts

With raw Jobs, users would need to manually:
- Create ConfigMaps for each content source
- Configure multiple Volume and VolumeMount entries
- Handle content aggregation logic themselves

#### Agent Reuse

Agent centralizes execution environment configuration:

```yaml
apiVersion: kubeopencode.io/v1alpha1
kind: Agent
metadata:
  name: default
spec:
  agentImage: quay.io/myorg/claude-agent:v2.0
  credentials:
    - name: github-token
      secretRef:
        name: github-creds
        key: token
      env: GITHUB_TOKEN
  scheduling:
    nodeSelector:
      workload-type: ai-agent
  serviceAccountName: kubeopencode-agent
```

Multiple Tasks share this configuration without repetition. With raw Jobs, each Job would need complete environment specification.

#### Batch Operations via Kubernetes-Native Tools

For running the same task across multiple targets, use Helm, Kustomize, or other templating tools:

```yaml
# Helm template example
{{- range .Values.tasks }}
---
apiVersion: kubeopencode.io/v1alpha1
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

This approach:
- Uses existing Kubernetes tooling
- Integrates with GitOps workflows
- Avoids adding custom batch orchestration CRDs
- Follows cloud-native best practices

### 3. Comparison Summary

| Aspect | Raw Kubernetes Job | Task CRD |
|--------|-------------------|----------|
| **Abstraction Level** | Generic container execution | AI-task-specific semantics |
| **Context Handling** | Manual ConfigMap/Volume setup | Automatic aggregation from multiple sources |
| **Environment Config** | Per-Job specification | Agent reference (reusable) |
| **Batch Execution** | Helm/Kustomize templating | Same - use Helm/Kustomize |
| **Extensibility** | Modify Job templates | Add new Context types |
| **API Semantics** | Container/Pod focused | Task/Workflow focused |
| **Learning Curve** | Requires K8s Volume knowledge | Domain-focused API |

### 4. Why We Removed Batch/BatchRun CRDs

Initially, KubeOpenCode included Batch and BatchRun CRDs for batch operations. We removed them because:

1. **Kubernetes-native alternative exists**: Helm, Kustomize, and other templating tools already solve the "create multiple similar resources" problem
2. **Reduced complexity**: Two fewer CRDs to maintain and document
3. **Better tooling integration**: Works naturally with ArgoCD, Flux, and other GitOps tools
4. **Separation of concerns**: KubeOpenCode focuses on task execution, templating is handled by external tools

## Consequences

### Positive

- **Clear Domain Model**: Users think in terms of "Tasks" not "Jobs with specific configurations"
- **Reduced Boilerplate**: Context aggregation eliminates manual ConfigMap/Volume setup
- **Simple API**: Only two CRDs (Task and Agent) to learn
- **Separation of Concerns**: WHAT (Task) + HOW (Agent)
- **GitOps Ready**: Declarative resources work naturally with GitOps tools
- **Kubernetes-native Batch**: Use Helm/Kustomize for batch operations
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
- [KubeOpenCode Architecture](../architecture.md)
