# ADR 0005: Human-in-the-Loop Session Strategies

## Status

Accepted

## Context

AI agents executing Tasks often need human involvement for review, debugging, or manual intervention. KubeOpenCode needs to support Human-in-the-Loop (HITL) scenarios where users can interact with the agent's workspace after task completion.

### Requirements

1. **Interactive Access**: Users need `kubectl exec` access to inspect and modify workspace
2. **Same Environment**: Interactive session should have same tools, credentials, and context as the agent
3. **Resource Efficiency**: Minimize resource consumption when sessions are not active
4. **Flexibility**: Support both immediate interaction and delayed/resumed sessions

### Use Cases

| Scenario | Timing | Duration |
|----------|--------|----------|
| Real-time debugging | Immediate after agent completes | Minutes to hours |
| Code review | Within same work session | Hours |
| Iterative development | Multiple sessions over days | Days |
| Collaborative review | Different users at different times | Days to weeks |

## Decision

We decided to implement **two complementary session strategies** that can be used together:

### Strategy 1: Session Sidecar (Ephemeral)

A sidecar container that runs alongside the agent, providing **immediate but temporary** access.

```
┌─────────────────────────────────────────────────────┐
│                      Task Pod                        │
│  ┌─────────────────┐  ┌─────────────────────────┐   │
│  │     agent       │  │        session          │   │
│  │  (main work)    │  │  (sleep for duration)   │   │
│  │  exits when     │  │  keeps Pod alive        │   │
│  │  done           │  │  for kubectl exec       │   │
│  └─────────────────┘  └─────────────────────────┘   │
│         │                        │                   │
│         └── shared workspace ────┘                   │
└─────────────────────────────────────────────────────┘
```

**Characteristics:**
- Available immediately after agent completes
- Temporary: expires after configured duration
- Workspace lost when Pod terminates
- Lower setup complexity

### Strategy 2: Session Persistence (Durable)

Workspace content is saved to a PVC, enabling **resume from a new Pod** after original Pod terminates.

```
Phase 1: Task Execution
┌─────────────────────────────────────────────────────────────┐
│                         Task Pod                             │
│  ┌───────────┐  ┌───────────┐  ┌─────────────────────────┐  │
│  │   agent   │  │  session  │  │     save-session        │  │
│  │           │  │ (sidecar) │  │  (copies workspace)     │  │
│  └───────────┘  └───────────┘  └─────────────────────────┘  │
│        │                               │                     │
│        └─── signal file ───────────────►                     │
└─────────────────────────────────────────────────────────────┘
                                         │
                                         ▼ save to PVC
                     ┌─────────────────────────────────────────┐
                     │        Session PVC                       │
                     │   /<namespace>/<task-name>/              │
                     └─────────────────────────────────────────┘

Phase 2: Session Resume (triggered by annotation)
┌─────────────────────────────────────────────────────────────┐
│                      Session Pod                             │
│  ┌─────────────────────────────────────────────────────────┐│
│  │                       session                            ││
│  │    - Same image, credentials, env                        ││
│  │    - Mounts workspace from PVC                           ││
│  └─────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────┘
```

**Characteristics:**
- Workspace persists after Pod termination
- Can resume days or weeks later
- Multiple resume sessions possible
- Requires PVC with ReadWriteMany (RWX) access

### Comparison

| Aspect | Session Sidecar | Session Persistence |
|--------|-----------------|---------------------|
| Availability | Immediate | After annotation trigger |
| Duration | Fixed (e.g., 1h) | Unlimited (within retention) |
| Workspace after timeout | Lost | Preserved |
| Resource when idle | Pod running | Only PVC storage |
| Resume capability | No | Yes |
| Setup complexity | Low | Medium (requires PVC) |
| Cost | Higher (continuous Pod) | Lower (storage only) |

### Configuration Locations

The two strategies are configured in the same Agent resource but can be enabled independently:

| Strategy | Configuration Location | Rationale |
|----------|----------------------|-----------|
| Session Sidecar | `Agent.spec.humanInTheLoop.sidecar.enabled` | Ephemeral session for immediate access |
| Session Persistence | `Agent.spec.humanInTheLoop.persistence.enabled` | Durable workspace saved to PVC |
| PVC Infrastructure | `KubeOpenCodeConfig.spec.sessionPVC` | System-level PVC configuration |

### Combined Behavior Matrix

| sidecar.enabled | persistence.enabled | sessionPVC configured | Behavior |
|-----------------|---------------------|----------------------|----------|
| `false` | `false` | - | Standard Task execution, no session |
| `true` | `false` | - | Session sidecar only (ephemeral) |
| `false` | `true` | Yes | Persistence only (save workspace, resume later) |
| `true` | `true` | Yes | Both strategies active (full capability) |

When both are enabled:
1. Session sidecar provides immediate access
2. save-session sidecar copies workspace to PVC on agent completion
3. User can continue using session sidecar OR let it timeout
4. Later, user can trigger resume via annotation to create new session Pod

## Implementation Details

### Session Sidecar (Agent Configuration)

```yaml
apiVersion: kubeopencode.io/v1alpha1
kind: Agent
metadata:
  name: dev-agent
spec:
  agentImage: quay.io/kubeopencode/kubeopencode-agent-gemini:latest
  command: ["sh", "-c", "gemini -p \"$(cat /workspace/task.md)\""]
  workspaceDir: /workspace
  serviceAccountName: kubeopencode-agent
  humanInTheLoop:
    # Shared configuration (used by both sidecar and resumed session Pod)
    image: ""             # Optional: defaults to agentImage
    command: []           # Optional: custom command (mutually exclusive with duration)
    ports:                # Optional: for port-forwarding
      - name: dev-server
        containerPort: 3000

    # Session Sidecar (ephemeral)
    sidecar:
      enabled: true
      duration: "2h"      # How long sidecar runs

    # Session Persistence (durable)
    persistence:
      enabled: false      # Set to true to save workspace to PVC
```

**Key Fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `image` | string | agentImage | Container image for session containers (shared) |
| `command` | []string | - | Custom command for session containers (shared) |
| `ports` | []ContainerPort | - | Ports to expose for port-forwarding (shared) |
| `sidecar.enabled` | bool | false | Enable session sidecar |
| `sidecar.duration` | Duration | 1h | How long sidecar runs (ignored if command is set) |
| `persistence.enabled` | bool | false | Enable workspace persistence to PVC |

### Session PVC Configuration (KubeOpenCodeConfig)

KubeOpenCodeConfig provides the **PVC infrastructure** for session persistence. Whether persistence is enabled is controlled per-Agent via `humanInTheLoop.persistence.enabled`.

```yaml
apiVersion: kubeopencode.io/v1alpha1
kind: KubeOpenCodeConfig
metadata:
  name: default
  namespace: kubeopencode-system
spec:
  sessionPVC:
    name: kubeopencode-session-data    # PVC name
    storageClassName: ""           # Empty = cluster default
    storageSize: "50Gi"
    retentionPolicy:
      ttlSecondsAfterTaskDeletion: 604800  # 7 days
```

**Key Fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | kubeopencode-session-data | Name of shared PVC |
| `storageClassName` | string | "" | StorageClass for PVC (empty = default) |
| `storageSize` | string | 10Gi | Size of PVC |
| `retentionPolicy.ttlSecondsAfterTaskDeletion` | int32 | 604800 | How long to keep session data after Task deletion |

**Note:** This only configures the PVC infrastructure. To enable persistence for a Task, set `humanInTheLoop.persistence.enabled: true` on the Agent.

### Save Mechanism

When `persistence.enabled` is true and `sessionPVC` is configured in KubeOpenCodeConfig, a `save-session` sidecar is added to the Pod:

1. Agent command is wrapped to create `/signal/.agent-done` on exit
2. save-session sidecar waits for this signal file
3. When detected, copies workspace to PVC at `/<namespace>/<task-name>/`
4. Sidecar exits after copy completes

### Resume Mechanism

To resume a session:

```bash
# Trigger resume via annotation
kubectl annotate task my-task kubeopencode.io/resume-session=true
```

Controller creates a session Pod:
- Name: `<task-name>-session`
- Labels: `kubeopencode.io/session-task=<task-name>`, `kubeopencode.io/component=session`
- OwnerReference: Points to Task (garbage collected with Task)
- Workspace: Mounted from PVC subdirectory

### Session Status Tracking

Task status includes session information:

```yaml
status:
  phase: Completed
  sessionPodName: my-task-session
  sessionStatus:
    phase: Active         # Pending | Active | Terminated
    startTime: "2025-01-18T10:00:00Z"
    workspacePath: "/default/my-task"
```

## Consequences

### Positive

- **Flexibility**: Choose ephemeral or persistent sessions based on needs
- **Cost Optimization**: Persistent sessions reduce resource usage for long-running work
- **Resume Capability**: Users can return to work after extended breaks
- **Collaboration**: Multiple users can access persisted workspaces
- **Backward Compatible**: Existing HITL behavior unchanged unless persistence enabled

### Negative

- **Complexity**: Two session strategies to understand
- **PVC Requirement**: Persistence requires RWX storage
- **Additional Container**: save-session sidecar adds to Pod spec
- **Storage Cost**: Persistent sessions consume storage

### Mitigations

- Clear documentation differentiating the two strategies
- Both features are opt-in (disabled by default)
- Helm chart option to auto-provision PVC
- Configurable retention TTL for cleanup

## Usage Examples

### Scenario 1: Quick Debugging (Sidecar Only)

```yaml
# Agent with short session duration
apiVersion: kubeopencode.io/v1alpha1
kind: Agent
metadata:
  name: debug-agent
spec:
  agentImage: quay.io/kubeopencode/kubeopencode-agent-gemini:latest
  serviceAccountName: kubeopencode-agent
  humanInTheLoop:
    sidecar:
      enabled: true
      duration: "30m"
```

```bash
# After Task completes, exec into session container
kubectl exec -it <pod-name> -c session -- /bin/bash

# Exit when done - workspace is lost
```

### Scenario 2: Persistence Only (No Sidecar)

```yaml
# KubeOpenCodeConfig with PVC configuration
apiVersion: kubeopencode.io/v1alpha1
kind: KubeOpenCodeConfig
metadata:
  name: default
spec:
  sessionPVC:
    name: kubeopencode-session-data
    storageSize: "50Gi"
---
# Agent with persistence only (no sidecar)
apiVersion: kubeopencode.io/v1alpha1
kind: Agent
metadata:
  name: persist-agent
spec:
  agentImage: quay.io/kubeopencode/kubeopencode-agent-gemini:latest
  serviceAccountName: kubeopencode-agent
  humanInTheLoop:
    persistence:
      enabled: true
    # No sidecar - workspace is saved but no immediate access
```

```bash
# Task completes, workspace is saved to PVC
# Resume later via annotation
kubectl annotate task my-task kubeopencode.io/resume-session=true

# Access session Pod
kubectl exec -it my-task-session -c session -- /bin/bash
```

### Scenario 3: Long-term Development (Both Strategies)

```yaml
# KubeOpenCodeConfig with PVC configuration
apiVersion: kubeopencode.io/v1alpha1
kind: KubeOpenCodeConfig
metadata:
  name: default
spec:
  sessionPVC:
    name: kubeopencode-session-data
---
# Agent with both sidecar and persistence
apiVersion: kubeopencode.io/v1alpha1
kind: Agent
metadata:
  name: dev-agent
spec:
  agentImage: quay.io/kubeopencode/kubeopencode-agent-gemini:latest
  serviceAccountName: kubeopencode-agent
  humanInTheLoop:
    sidecar:
      enabled: true
      duration: "2h"
    persistence:
      enabled: true
```

```bash
# Day 1: Work in session sidecar
kubectl exec -it <pod-name> -c session -- /bin/bash
# ... do work, leave for the day

# Day 2: Resume via annotation
kubectl annotate task my-task kubeopencode.io/resume-session=true

# Access new session Pod
kubectl exec -it my-task-session -c session -- /bin/bash

# When completely done, stop the session
kubectl annotate task my-task kubeopencode.io/stop=true
```

### Scenario 4: Code-Server with Persistence

```yaml
apiVersion: kubeopencode.io/v1alpha1
kind: Agent
metadata:
  name: codeserver-agent
spec:
  agentImage: quay.io/kubeopencode/kubeopencode-agent-gemini:latest
  serviceAccountName: kubeopencode-agent
  humanInTheLoop:
    # Shared config for both sidecar and resumed session
    image: quay.io/kubeopencode/kubeopencode-agent-code-server:latest
    command:
      - sh
      - -c
      - code-server --bind-addr 0.0.0.0:8080 ${WORKSPACE_DIR} & sleep 86400
    ports:
      - name: code-server
        containerPort: 8080
    # Enable both strategies
    sidecar:
      enabled: true
    persistence:
      enabled: true
```

```bash
# Access code-server via port-forward
kubectl port-forward <pod-name> 8080:8080

# After timeout, resume with same workspace
kubectl annotate task my-task kubeopencode.io/resume-session=true
kubectl port-forward my-task-session 8080:8080
```

## Future Extensions

- **Session Snapshots**: Save named snapshots of workspace state
- **Session Sharing**: Explicitly share session data between Tasks
- **Session Cleanup Controller**: Background cleanup of expired sessions
- **Session Metrics**: Usage tracking for capacity planning

## References

- [KubeOpenCode Architecture - Human-in-the-Loop](../architecture.md#human-in-the-loop)
- [Kubernetes Jobs - Suspending a Job](https://kubernetes.io/docs/concepts/workloads/controllers/job/#suspending-a-job)
- [Kubernetes PersistentVolumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)
