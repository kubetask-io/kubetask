# ADR 0007: Migrate Workflow and Webhook Functionality to Argo

## Status

Accepted

## Context

KubeOpenCode initially implemented several CRDs for workflow orchestration and event-driven triggers:

- **Workflow**: Multi-stage task templates
- **WorkflowRun**: Workflow execution instances
- **CronWorkflow**: Scheduled workflow triggers
- **WebhookTrigger**: Event-driven Task creation from webhooks

Additionally, KubeOpenCode included:
- A webhook server for receiving and processing webhooks
- Session persistence and humanInTheLoop features for interactive debugging

As the project matured, we identified several issues:

1. **Duplicating Existing Solutions**: Argo Workflows and Argo Events are mature, well-tested projects that already provide workflow orchestration and event-driven triggers. Maintaining our own implementations adds unnecessary complexity.

2. **Maintenance Burden**: The webhook server, workflow controllers, and session persistence code required significant maintenance effort, diverting resources from the core Task/Agent functionality.

3. **Feature Gap**: Our implementations lacked features available in Argo (retry strategies, advanced DAG execution, extensive event sources, etc.).

4. **Scope Creep**: KubeOpenCode's value proposition is the Task/Agent abstraction for AI workloads. Workflow orchestration and event handling are orthogonal concerns.

5. **Unused Features**: Session persistence and humanInTheLoop were experimental features that saw limited adoption.

## Decision

**Migrate workflow orchestration and webhook handling to Argo Workflows and Argo Events, while keeping KubeOpenCode focused on the core Task and Agent CRDs.**

### What We Remove

1. **CRDs**:
   - Workflow
   - WorkflowRun
   - CronWorkflow
   - WebhookTrigger

2. **Controllers**:
   - workflow_controller.go
   - workflowrun_controller.go
   - cronworkflow_controller.go
   - webhooktrigger_controller.go

3. **Webhook Server**:
   - internal/webhook/ (server, auth, filter, template)
   - cmd/kubeopencode/webhook.go

4. **Session Features**:
   - humanInTheLoop in Agent spec
   - SessionPVC in KubeOpenCodeConfig
   - TaskLifecycle TTL configuration
   - save-session subcommand

### What We Keep

1. **Core CRDs**:
   - Task (the primary API)
   - Agent (execution configuration)
   - KubeOpenCodeConfig (simplified to systemImage only)

2. **Subcommands**:
   - controller (Task reconciliation)
   - git-init (Git context cloning)
   - context-init (context file initialization)

### Integration Pattern

Argo Events creates KubeOpenCode Tasks (not Argo Workflows directly), preserving the Task/Agent abstraction:

```
GitHub Webhook → Argo Events (EventSource) → Sensor → kubectl create Task → Task Controller → Job
```

This pattern:
- Keeps KubeOpenCode as the execution layer for AI tasks
- Leverages Argo Events for event routing and filtering
- Allows users to use Argo Workflows for complex orchestration if needed

### Dogfooding Migration

The dogfooding environment at `deploy/dogfooding/` is updated:

- **Removed**: `webhooktrigger-github.yaml`, `workflow-go-update.yaml`
- **Added**: `argo-events/` directory with EventBus, EventSource, Sensor, and RBAC
- **Updated**: smee-client and OpenShift route to point to Argo EventSource

## Consequences

### Positive

1. **Reduced Maintenance**: ~50% reduction in controller code, no webhook server to maintain
2. **Better Features**: Users get access to Argo's full feature set (complex DAGs, retry strategies, 40+ event sources)
3. **Clear Focus**: KubeOpenCode has a clearer value proposition - AI task execution
4. **Proven Ecosystem**: Argo is a CNCF graduated project with extensive community support
5. **Simpler API**: Fewer CRDs to learn, simpler mental model

### Negative

1. **Additional Dependency**: Users need to install Argo Events/Workflows separately
2. **Migration Effort**: Existing WebhookTrigger users must migrate to Argo Events Sensors
3. **Learning Curve**: Users unfamiliar with Argo must learn its concepts
4. **Lost Session Features**: humanInTheLoop and session persistence are removed (were experimental)

### Migration Path

For users with existing WebhookTrigger resources:

1. Install Argo Events in the cluster
2. Convert WebhookTrigger rules to Sensor triggers
3. The Sensor should create KubeOpenCode Tasks (same agentRef, description, contexts)
4. Delete the old WebhookTrigger resources

Example migration:

**Before (WebhookTrigger)**:
```yaml
apiVersion: kubeopencode.io/v1alpha1
kind: WebhookTrigger
metadata:
  name: github
spec:
  auth:
    hmac:
      secretRef:
        name: github-webhook-secret
        key: hmacKey
  rules:
    - name: pr-opened
      filter: 'headers["x-github-event"] == "pull_request" && body.action == "opened"'
      resourceTemplate:
        task:
          agentRef: default
          description: "Review PR..."
```

**After (Argo Events Sensor)**:
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Sensor
metadata:
  name: github-tasks
spec:
  dependencies:
    - name: github-event
      eventSourceName: github
      eventName: kubeopencode
  triggers:
    - template:
        name: pr-opened
        conditions: pr-opened-condition
        k8s:
          operation: create
          source:
            resource:
              apiVersion: kubeopencode.io/v1alpha1
              kind: Task
              metadata:
                generateName: github-pr-opened-
              spec:
                agentRef: default
                description: ""
          parameters:
            - src:
                dependencyName: github-event
                dataTemplate: "Review PR..."
              dest: spec.description
  conditions:
    - name: pr-opened-condition
      expression: >-
        Input.headers["X-Github-Event"] == "pull_request" &&
        Input.body.action == "opened"
```

## References

- [Argo Events Documentation](https://argoproj.github.io/argo-events/)
- [Argo Workflows Documentation](https://argoproj.github.io/argo-workflows/)
- [Migration Plan](../../.claude/plans/polished-tickling-parnas.md)
