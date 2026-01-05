# ADR 0009: Task Execution Migration from Job to Pod

## Status

Accepted (2026-01-05)

## Context

KubeOpenCode originally used Kubernetes Jobs as the execution primitive for Tasks. This decision was documented in [ADR 0002](0002-task-crd-vs-kubernetes-job.md). However, after reviewing the implementation and comparing with similar projects like Argo Workflows, we identified that the Job abstraction layer was not providing significant value.

### Current State Analysis

The existing Job-based implementation used:
- `BackoffLimit: 0` - No retry on failure
- `RestartPolicy: Never` - Pod never restarts

These settings meant that Job-specific features (retry logic, parallelism, completions) were not being utilized. The Job was essentially a thin wrapper around a Pod.

### Argo Workflows Comparison

Argo Workflows, a mature Kubernetes-native workflow engine, uses Pods directly for workflow step execution rather than Jobs. This approach:
- Simplifies architecture (fewer abstraction layers)
- Provides more direct control over Pod lifecycle
- Reduces API calls (no Job controller reconciliation overhead)

## Decision

We decided to migrate Task execution from creating Kubernetes Jobs to directly creating Pods.

### Changes Made

1. **Core Builder**: `buildJob()` renamed to `buildPod()`, now returns `*corev1.Pod`
2. **Status Tracking**: `updateTaskStatusFromJob()` renamed to `updateTaskStatusFromPod()`, tracks `Pod.Status.Phase`
3. **API Field**: `TaskExecutionStatus.JobName` renamed to `TaskExecutionStatus.PodName`
4. **Stop Handling**: Changed from Job suspend to Pod deletion
5. **RBAC**: Removed `batch/jobs` permissions, kept `pods` permissions
6. **Owner References**: Pod is now directly owned by Task (was Job → Pod)

### Stop Annotation Behavior Change

**Before (Job-based)**:
- Set `Job.Spec.Suspend = true`
- Job and Pod preserved for log access

**After (Pod-based)**:
- Delete the Pod directly (with graceful termination)
- Pod and logs are removed after deletion

This follows the same pattern as Argo Workflows. Users requiring log persistence should use external log aggregation systems (Loki, ELK, CloudWatch, etc.).

### Pod Status Mapping

| Pod.Status.Phase | Task.Status.Phase |
|------------------|-------------------|
| Pending/Running  | Running           |
| Succeeded        | Completed         |
| Failed           | Failed            |

Additional detection:
- Pod eviction: `Pod.Status.Reason == "Evicted"` → Task Failed
- Pod preemption: `Pod.Status.Reason == "Preempted"` → Task Failed
- Pod not found (while Task is Running): Task Failed

## Consequences

### Positive

- **Simpler Architecture**: One less abstraction layer (no Job controller involvement)
- **Faster Execution**: Direct Pod creation eliminates Job controller reconciliation delay
- **Cleaner Ownership**: Direct Task → Pod ownership reference
- **Reduced RBAC Surface**: No need for `batch/jobs` permissions
- **Alignment with Industry Patterns**: Matches Argo Workflows approach

### Negative

- **Log Loss on Stop**: Logs are lost when a Task is stopped via annotation
  - *Mitigation*: Document requirement for external log aggregation
- **API Breaking Change**: `JobName` field renamed to `PodName`
  - *Mitigation*: This is a v1alpha1 API, breaking changes are expected

### Neutral

- **External Pod Deletion**: External deletion of Pods will cause Task to fail
  - Same behavior as before with Jobs

## References

- [ADR 0002: Task CRD vs Kubernetes Job](0002-task-crd-vs-kubernetes-job.md) (superseded by this ADR)
- [Argo Workflows Pod Execution](https://argoproj.github.io/argo-workflows/)
- [Kubernetes Pod Lifecycle](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/)
