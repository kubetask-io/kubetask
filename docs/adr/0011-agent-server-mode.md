# ADR 0011: Agent Server Mode for Persistent OpenCode Servers

## Status

Accepted

## Context

KubeOpenCode currently operates in "Pod mode" where each Task creates a new Pod that runs to completion. This ephemeral approach has several limitations for certain use cases:

### The Problem

1. **Cold Start Latency**: Every Task requires container startup time, including image pull, init container execution, and OpenCode initialization. For interactive use cases (e.g., Slack bots), this delay is unacceptable.

2. **No Shared Context**: Each Task runs in isolation. Pre-loaded repositories, cached dependencies, or warmed-up model contexts cannot be shared across Tasks.

3. **Resource Inefficiency**: Long-running agents that need to respond quickly to events waste resources starting and stopping containers repeatedly.

### Use Case: Slack Bot with Repository Context

A typical use case driving this decision:
- A Slack bot needs to answer questions about a company's codebase
- The bot should respond within seconds, not minutes
- Multiple Tasks (one per Slack message) should share the same pre-cloned repository
- The agent needs to maintain context across conversations

### OpenCode Serve Functionality

OpenCode provides a `serve` command that runs a persistent HTTP server:

```bash
opencode serve --port 4096 --hostname 0.0.0.0
```

Key endpoints:
- `POST /session` — Create a new session
- `POST /session/:id/message` — Send message to session (streaming)
- `POST /session/:id/prompt_async` — Send message asynchronously
- `GET /session/status` — Get all session statuses
- `DELETE /session/:id` — Delete a session

This server model enables persistent agent execution with session management.

## Decision

### Add Server Mode to Agent API

We extend the existing Agent API with an optional `serverConfig` field. The presence of this field determines the execution mode:

- **No `serverConfig`** → Pod mode (default, current behavior)
- **Has `serverConfig`** → Server mode (persistent OpenCode server)

```yaml
apiVersion: kubeopencode.io/v1alpha1
kind: Agent
metadata:
  name: slack-agent
spec:
  executorImage: quay.io/kubeopencode/kubeopencode-agent-devbox:latest
  workspaceDir: /workspace
  serverConfig:
    port: 4096  # OpenCode server port (default: 4096)
  # Resource requirements are in podSpec (applies to both modes)
  podSpec:
    resources:
      requests:
        memory: "512Mi"
```

### Key Design Decisions

#### 1. Mode Detection via Field Presence

**Decision**: Infer mode from `serverConfig != nil` rather than an explicit `mode` field.

**Rationale**:
- Simplest possible API — existing Agents work unchanged
- No risk of inconsistency between `mode` and `serverConfig` values
- Natural extension pattern — add configuration when needed

#### 2. Pod-based Execution with `--attach` Flag

**Decision**: Use `opencode run --attach <server-url>` via lightweight Pods instead of HTTP client.

**Rationale**:
- Logs are preserved via `kubectl logs` (same as Pod mode)
- Reuses existing Pod creation logic, reducing code complexity
- Eliminates ~700 lines of HTTP client code
- Both modes now use the same execution pattern (create Pod, wait for completion)

#### 3. Single Replica Only (Initial)

**Decision**: Server mode uses a single replica Deployment initially.

**Rationale**:
- Simplifies implementation — no sticky session routing needed
- OpenCode sessions are stateful and not easily distributed
- Multi-replica support can be added later with session affinity

#### 4. 1 Task = 1 Pod (Unified Model)

**Decision**: Each Task creates exactly one Pod, regardless of mode.

**Rationale**:
- Intuitive mental model — same pattern for both modes
- Clean lifecycle management — Pod deletion via finalizer
- `maxConcurrentTasks` naturally limits concurrent pods

#### 5. Separate Agent Controller

**Decision**: Create a dedicated `AgentReconciler` for Server-mode infrastructure management.

**Rationale**:
- Separation of concerns — Task controller handles task lifecycle, Agent controller manages infrastructure
- Existing `task_controller.go` is 1600+ lines; adding Deployment logic would make it unwieldy
- Agent CRD already has status fields; adding server status is natural

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Agent Controller                         │
│  - Watches Agent resources                                   │
│  - For Server-mode: creates Deployment + Service             │
│  - Updates Agent.status.serverStatus                         │
│  - Health based on Deployment readiness                      │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                   Deployment (opencode serve)                │
│  - Single replica                                            │
│  - Runs: /tools/opencode serve --port 4096                  │
│  - Liveness: TCP check on port                              │
│  - Readiness: HTTP check on /session/status                 │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                     Task Controller                          │
│  - Detects Server-mode via agent.Spec.ServerConfig != nil   │
│  - Creates Pod with: opencode run --attach <server-url>     │
│  - Standard Pod status tracking (same as Pod mode)          │
│  - Pod deleted via finalizer                                  │
└─────────────────────────────────────────────────────────────┘
```

### Execution Flow Comparison

**Pod Mode:**
```
Task Created → Create Pod → opencode run "task" → Pod completes → Task done
```

**Server Mode:**
```
Task Created → Create Pod → opencode run --attach <url> "task" → Pod completes → Task done
```

Both modes use the same Pod lifecycle, with the difference being the `--attach` flag.

### Status Mapping

| Pod Status            | Task Phase |
|-----------------------|------------|
| Pod created           | Pending    |
| Pod running           | Running    |
| Pod succeeded         | Completed  |
| Pod failed            | Failed     |

## Consequences

### Positive

1. **No Cold Start**: Server is already running; Tasks execute immediately
2. **Shared Context**: Pre-loaded repos and caches are available to all Tasks
3. **Backward Compatible**: Existing Agents work unchanged (Pod mode is default)
4. **Unified Task API**: Task authors don't need to know about server mode
5. **Natural Resource Limits**: `maxConcurrentTasks` limits sessions on the server

### Negative

1. **Persistent Resource Usage**: Server runs even when no Tasks are active
2. **Single Point of Failure**: All Tasks depend on one server instance
3. **No Horizontal Scaling**: Single replica limits throughput (initial version)

### Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Server crashes | Deployment ensures restart; Tasks fail gracefully |
| Stale server | Deployment readiness probe checks health |
| Resource exhaustion | `maxConcurrentTasks` limits active pods |

## Implementation

### Files Created
- `internal/controller/agent_controller.go` — Agent reconciliation (Deployment/Service management)
- `internal/controller/server_builder.go` — Helper functions for server mode

### Files Modified
- `api/v1alpha1/agent_types.go` — Added `ServerConfig`, `ServerStatus`
- `internal/controller/pod_builder.go` — Added `serverURL` parameter for `--attach` command
- `internal/controller/task_controller.go` — Unified Pod-based execution for both modes
- `cmd/kubeopencode/controller.go` — Register Agent controller

## Future Considerations

1. **Multi-replica Support**: Add session affinity for horizontal scaling
2. **Session Persistence**: Survive server restarts with session state
3. **WebSocket Support**: Real-time streaming instead of polling
4. **Automatic Scaling**: Scale replicas based on session count

## References

- OpenCode serve command: `../opencode/packages/opencode/src/cli/cmd/serve.ts`
- OpenCode session API: `../opencode/packages/opencode/src/session/`
- Plan document: `.claude/plans/prancy-soaring-sutherland.md`
