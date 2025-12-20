# Claude Development Guidelines for KubeTask

> **Note**: `AGENTS.md` is a symbolic link to this file (`CLAUDE.md`), ensuring both files are always identical.

This document provides guidelines for AI assistants (like Claude) working on the KubeTask project.

## Project Overview

KubeTask is a Kubernetes-native system that executes AI-powered tasks using Custom Resources (CRs) and the Operator pattern. It provides a simple, declarative way to run AI agents (like Claude, Gemini) as Kubernetes Jobs.

**Key Technologies:**
- Kubernetes Custom Resource Definitions (CRDs)
- Controller Runtime (kubebuilder)
- Go 1.25
- Helm for deployment

**Architecture Philosophy:**
- No external dependencies (no PostgreSQL, Redis)
- Kubernetes-native (uses etcd for state, Jobs for execution)
- Declarative and GitOps-friendly
- Simple API: Task (WHAT to do) + Agent (HOW to execute)
- Use Helm/Kustomize for batch operations (multiple Tasks)

**Unified Binary:**

KubeTask uses a single container image (`quay.io/kubetask/kubetask`) with multiple subcommands:

| Subcommand | Used For |
|------------|----------|
| `controller` | Main controller reconciliation |
| `git-init` | Git Context cloning (init container) |
| `save-session` | Session persistence (sidecar) |
| `webhook` | Webhook server for WebhookTrigger |

The image constant is defined in `internal/controller/job_builder.go` as `DefaultKubeTaskImage`.

## Core Concepts

### Resource Hierarchy

1. **Task** - Single task execution (the primary API)
2. **Workflow** - Reusable multi-stage task template
3. **WorkflowRun** - Workflow execution instance
4. **CronWorkflow** - Scheduled WorkflowRun triggering
5. **WebhookTrigger** - Event-driven Task creation from webhooks
6. **Agent** - AI agent configuration (HOW to execute)
7. **Context** - Reusable context resources (Inline, ConfigMap, or Git)

### Important Design Decisions

- **Agent** (not KubeTaskConfig) - Stable, project-independent naming
- **AgentImage** (not AgentTemplateRef) - Simple container image, controller generates Jobs
- **agentRef** - Reference from Task to Agent
- **No Batch/BatchRun** - Use Helm/Kustomize to create multiple Tasks (Kubernetes-native approach)

### Context System

Tasks use the **Context CRD** to reference reusable context resources:
- **Inline**: Content directly in YAML (`spec.inline.content`)
- **ConfigMap**: Content from ConfigMap (`spec.configMap.name` + optional `key`)
- **Git**: Content from Git repository (`spec.git.repository`, `path`, `ref`, `secretRef`)

**ContextMount** is used in Task/Agent to reference Context CRDs:
- `name`: Name of the Context CRD
- `mountPath`: Where to mount (empty = append to task.md with XML tags)
  - Path resolution follows Tekton conventions:
    - Absolute paths (`/etc/config`) are used as-is
    - Relative paths (`guides/readme.md`) are prefixed with workspaceDir

**Future**: MCP contexts (extensible design)

## Code Standards

### File Headers

All Go files must include the copyright header:

```go
// Copyright Contributors to the KubeTask project
```

### Naming Conventions

1. **API Resources**: Use semantic names independent of project name
   - Good: `Agent`, `AgentTemplateRef`
   - Avoid: `KubeTaskConfig`, `JobTemplateRef`

2. **Go Code**: Follow standard Go conventions
   - Package names: lowercase, single word
   - Exported types: PascalCase
   - Unexported: camelCase

3. **Kubernetes Resources**:
   - CRD Group: `kubetask.io`
   - API Version: `v1alpha1`
   - Kinds: `Task`, `Workflow`, `WorkflowRun`, `CronWorkflow`, `WebhookTrigger`, `Agent`, `Context`, `KubeTaskConfig`

### Code Comments

- Write all comments in English
- Document exported types and functions
- Use godoc format for package documentation
- Include examples in comments where helpful

## Development Workflow

### Building and Testing

```bash
# Build the controller
make build

# Run tests
make test

# Run linter
make lint

# Update generated code (CRDs, deepcopy)
make update

# Verify generated code is up to date
make verify
```

### Local Development

```bash
# Run controller locally (requires kubeconfig)
make run

# Format code
make fmt
```

### E2E Testing

> **CRITICAL FOR AI ASSISTANTS**: When the user asks to run "e2e tests", "e2e testing", "test e2e", or any variation, you MUST execute all three commands in sequence. NEVER run `make e2e-test` alone - this will cause failures due to stale cluster state.

**Required E2E test flow** (always execute all three steps):

```bash
# Step 1: Clean up existing Kind cluster
make e2e-teardown

# Step 2: Setup complete e2e environment
make e2e-setup

# Step 3: Run e2e tests
make e2e-test
```

For iterative development only (when you've already run the full flow once in this session):
```bash
make e2e-reload  # Rebuild and reload controller image, then run e2e-test
```

### Docker and Registry

```bash
# Build docker image
make docker-build

# Push docker image
make docker-push

# Multi-arch build and push
make docker-buildx
```

### Cluster Deployment

> **CRITICAL**: Always deploy KubeTask to the `kubetask-system` namespace. This is the standard namespace used throughout all documentation and examples. Never use `kubetask` or other namespace names.

```bash
# Create namespace
kubectl create namespace kubetask-system

# Install with Helm (with webhook enabled)
helm install kubetask ./charts/kubetask \
  --namespace kubetask-system \
  --set webhook.enabled=true

# Or install from OCI registry
helm install kubetask oci://quay.io/kubetask/helm-charts/kubetask \
  --namespace kubetask-system \
  --set webhook.enabled=true
```

### Agent Images

Agent images are located in `agents/`. Each subdirectory contains a Dockerfile for a specific AI agent (gemini, claude, codex, etc.).

```bash
# Build specific agent (default: gemini)
make agent-build                    # Build gemini agent
make agent-build AGENT=gemini       # Build gemini agent (explicit)
make agent-build AGENT=claude       # Build claude agent
make agent-build AGENT=echo         # Build echo agent (for testing)

# Push agent image to registry
make agent-push                     # Push gemini agent
make agent-push AGENT=claude        # Push claude agent

# Multi-arch build and push
make agent-buildx                   # Multi-arch build gemini
make agent-buildx AGENT=claude      # Multi-arch build claude

# List available agents
make -C agents list
```

The agent images are tagged as `quay.io/kubetask/kubetask-agent-<AGENT>:latest` by default. You can customize the registry, org, and version:

```bash
make agent-build AGENT=gemini IMG_REGISTRY=docker.io IMG_ORG=myorg VERSION=v1.0.0
```

## Key Files and Directories

```
kubetask/
├── agents/               # Agent images and tools
│   ├── images/          # Agent Dockerfiles (gemini, claude, echo, etc.)
│   └── tools/           # Tools image for shared CLI tools
├── api/v1alpha1/          # CRD type definitions
│   ├── types.go           # Main API types (Task, Workflow, WorkflowRun, CronWorkflow, Agent, Context, KubeTaskConfig)
│   ├── webhooktrigger_types.go  # WebhookTrigger CRD types
│   ├── register.go        # Scheme registration
│   └── zz_generated.deepcopy.go  # Generated deepcopy
├── cmd/kubetask/          # Unified binary entry point
│   ├── main.go            # Root command
│   ├── controller.go      # Controller subcommand
│   ├── webhook.go         # Webhook server subcommand
│   ├── git_init.go        # Git init container subcommand
│   └── save_session.go    # Session persistence subcommand
├── internal/controller/   # Controller reconcilers
│   ├── task_controller.go
│   ├── workflow_controller.go
│   ├── workflowrun_controller.go
│   ├── cronworkflow_controller.go
│   └── webhooktrigger_controller.go
├── internal/webhook/      # Webhook server
│   ├── server.go          # HTTP server for receiving webhooks
│   ├── auth.go            # Authentication (HMAC, Bearer, Header)
│   ├── filter.go          # CEL filter evaluation
│   └── template.go        # Go template rendering
├── deploy/               # Kubernetes manifests
│   └── crds/            # Generated CRD YAMLs
├── charts/kubetask/     # Helm chart
│   └── templates/
│       ├── controller/   # Controller deployment
│       └── webhook/      # Webhook server deployment (optional)
├── hack/                # Build and codegen scripts
├── docs/                # Documentation
│   ├── architecture.md  # Architecture documentation
│   └── adr/             # Architecture Decision Records
└── Makefile             # Build automation
```

## Making Changes

### API Changes (Add/Update/Delete Fields)

When making **any** changes to the API (adding, updating, or deleting fields):

1. Update `api/v1alpha1/types.go`
2. Add/update appropriate kubebuilder markers
3. Run `make update` to regenerate CRDs and deepcopy
4. Run `make verify` to ensure everything is correct
5. **Update documentation** in `docs/architecture.md`
6. **Update integration tests** in `internal/controller/*_test.go` to cover the API changes
7. **Update E2E tests** in `e2e/` to verify the changes work end-to-end

> **IMPORTANT**: API changes are incomplete without corresponding updates to documentation, integration tests, and E2E tests. All three must be updated together with any API modification.

### Modifying Controllers

1. Update controller logic in `internal/controller/`
2. Ensure proper error handling and logging
3. Update status conditions appropriately
4. Test locally with `make run` or `make e2e-setup`

### Adding or Modifying Agents

When making **any** changes related to agents (adding new agents, modifying existing agent images, renaming agents, etc.):

1. Add/update agent files in `agents/<agent-name>/`
2. **Update GitHub workflow** in `.github/workflows/push.yaml`:
   - Add path filter for the new agent in the `changes` job
   - Add corresponding output variable
   - Add new build job for the agent image (following existing patterns)
3. Update `agents/README.md` if adding a new agent
4. Test the agent image build locally with `make agent-build AGENT=<name>`

> **IMPORTANT**: Agent changes are incomplete without updating the CI workflow. The workflow uses path-based filtering to conditionally build agent images, so new agents won't be built in CI unless added to the workflow.

### Updating CRDs

```bash
# After modifying api/v1alpha1/types.go
make update-crds

# This will:
# 1. Generate CRDs in deploy/crds/
# 2. Copy them to charts/kubetask/templates/crds/
```

## Testing Guidelines

This project uses a three-tier testing strategy:

### Test Types and Commands

```bash
# Unit tests (fast, no external dependencies)
make test

# Integration tests (uses envtest, requires kubebuilder binaries)
make integration-test

# E2E tests (uses Kind cluster, full system test)
make e2e-test
```

### Unit Tests

- Place tests alongside the code being tested
- Use table-driven tests where appropriate
- Mock Kubernetes client using controller-runtime fakes
- No special build tags required

### Integration Tests (envtest)

Integration tests use [envtest](https://book.kubebuilder.io/reference/envtest.html) to run a local API server and etcd, allowing controller testing without a full cluster.

**Build Tag Pattern**: We use `//go:build integration` to separate integration tests from unit tests. This is the **standard pattern in the Kubernetes ecosystem**, used by:
- [kubebuilder](https://github.com/kubernetes-sigs/kubebuilder) generated projects
- [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime)
- Most Kubernetes operator projects

**Why this pattern?**
- Tests remain close to the code they test (easier maintenance)
- Clear separation: `go test ./...` runs unit tests, `go test -tags=integration ./...` runs integration tests
- CI can run different test types in parallel
- Alternative (separate `test/integration/` directory) separates tests from code, making maintenance harder

**File structure**:
```
internal/controller/
├── task_controller.go           # Controller implementation
├── task_controller_test.go      # Integration tests (//go:build integration)
└── suite_test.go                # Test suite setup (//go:build integration)
```

### E2E Tests

- Located in `e2e/` directory
- Use Kind cluster for full system testing
- Test complete workflows (Task → Job)
- Verify status updates and conditions
- Check that cleanup works correctly

## Common Tasks

### Adding a New Context Type

1. Add new `ContextType` constant in `api/v1alpha1/types.go`
2. Add corresponding struct (e.g., `APIContext`, `DatabaseContext`)
3. Update `Context` struct with new optional field
4. Update controller to handle new context type
5. Update documentation

### Agent Configuration

Key Agent spec fields:
- `agentImage`: Container image for task execution
- `command`: **Required** - Entrypoint command that defines HOW the agent executes tasks
- `workspaceDir`: **Required** - Working directory where task.md and context files are mounted
- `contexts`: References to Context CRDs (applied to all tasks)
- `credentials`: Secrets as env vars or file mounts (supports single key or entire secret)
- `serviceAccountName`: Kubernetes ServiceAccount for RBAC
- `maxConcurrentTasks`: Limit concurrent Tasks using this Agent (nil/0 = unlimited)

**Command Field (Required):**

The `command` field is required and defines how the agent executes tasks. This design:
- Decouples agent images from execution logic (images provide tools, command defines usage)
- Allows users to customize execution behavior (e.g., output format, flags)

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: gemini-agent
spec:
  agentImage: quay.io/kubetask/kubetask-agent-gemini:latest
  command:
    - sh
    - -c
    - gemini --output-format stream-json --yolo -p "$(cat ${WORKSPACE_DIR}/task.md)"
  serviceAccountName: kubetask-agent
```

**Concurrency Control:**

When an Agent uses backend AI services with rate limits (e.g., Claude, Gemini API quotas),
you can limit concurrent Task execution using `maxConcurrentTasks`:

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: claude-agent
spec:
  agentImage: quay.io/kubetask/kubetask-agent-claude:latest
  command:
    - sh
    - -c
    - claude --output-format stream-json --dangerously-skip-permissions -p "$(cat ${WORKSPACE_DIR}/task.md)"
  serviceAccountName: kubetask-agent
  maxConcurrentTasks: 3  # Only 3 Tasks can run concurrently
```

When the limit is reached:
- New Tasks enter `Queued` phase instead of `Running`
- Tasks are labeled with `kubetask.io/agent: <agent-name>` for tracking
- Queued Tasks automatically transition to `Running` when capacity becomes available
- Tasks are processed in approximate FIFO order

**Task Stop:**

Running Tasks can be stopped by setting the `kubetask.io/stop=true` annotation:

```bash
kubectl annotate task my-task kubetask.io/stop=true
```

When this annotation is detected:
- The controller suspends the Job (sets `spec.suspend=true`)
- Kubernetes sends SIGTERM to all running Pods, triggering graceful shutdown
- Job and Pod are preserved (not deleted), so **logs remain accessible**
- Task status is set to `Completed` with a `Stopped` condition
- The `Stopped` condition has reason `UserStopped`

This is useful for:
- Exiting `humanInTheLoop` Tasks early after verification is complete
- Stopping long-running Tasks without waiting for timeout
- Preserving logs for debugging or auditing after stopping

**Session Strategies:**

KubeTask supports two complementary session strategies for human interaction:

1. **Session Sidecar (Ephemeral)**: A sidecar container runs alongside the agent for immediate access
2. **Session Persistence (Durable)**: Workspace is saved to PVC for later resume

Both are configured via `humanInTheLoop` and can be enabled independently:

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: dev-agent
spec:
  agentImage: quay.io/kubetask/kubetask-agent-gemini:latest
  serviceAccountName: kubetask-agent
  humanInTheLoop:
    # Shared config (used by both strategies)
    image: ""      # Optional: defaults to agentImage
    command: []    # Optional: custom command for session containers
    ports: []      # Optional: for port-forwarding

    # Ephemeral session sidecar
    sidecar:
      enabled: true
      duration: "2h"

    # Durable workspace persistence
    persistence:
      enabled: true  # Requires sessionPVC in KubeTaskConfig
```

To provide PVC infrastructure for session persistence, configure `sessionPVC` in KubeTaskConfig:

```yaml
apiVersion: kubetask.io/v1alpha1
kind: KubeTaskConfig
metadata:
  name: default
spec:
  sessionPVC:
    name: kubetask-session-data  # Shared PVC for all HITL tasks
    storageSize: 50Gi
```

When `persistence.enabled` is true and `sessionPVC` is configured:
- A `save-session` sidecar is added to Task Pods
- After agent completion, workspace is copied to PVC at `/<namespace>/<task-name>/`
- Users can resume work by adding annotation: `kubectl annotate task my-task kubetask.io/resume-session=true`
- A session Pod is created with the same image, credentials, and environment as the original agent
- Session Pod mounts the persisted workspace from PVC

Requirements:
- Agent must have `humanInTheLoop.persistence.enabled = true`
- KubeTaskConfig must have `sessionPVC` configured
- PVC must support ReadWriteMany (RWX) access mode for concurrent session access

**Credentials Mounting:**

Credentials can be mounted in two ways:

1. **Entire Secret** (all keys become ENV vars):
```yaml
credentials:
- name: api-keys
  secretRef:
    name: api-credentials
    # No key specified - all keys in secret become ENV vars
```

2. **Single Key** (with optional rename or file mount):
```yaml
credentials:
- name: github-token
  secretRef:
    name: github-creds
    key: token        # Specific key
  env: GITHUB_TOKEN   # Optional: rename the env var
- name: ssh-key
  secretRef:
    name: ssh-keys
    key: id_rsa
  mountPath: /home/agent/.ssh/id_rsa  # Mount as file
  fileMode: 0400
```

### Agent Image Discovery

The agent image is discovered via:
1. `Agent.spec.agentImage` (from referenced Agent)
2. Built-in default image (fallback: `quay.io/kubetask/kubetask-agent-gemini:latest`)

Agent lookup:
- Task uses `agentRef` to reference an Agent
- If not specified, looks for Agent named "default" in the same namespace
- If not found, uses built-in default image

The controller generates Jobs with:
- Labels: `kubetask.io/task`
- Env vars: `TASK_NAME`, `TASK_NAMESPACE`
- ServiceAccount from Agent spec
- Owner references for garbage collection

## Kubernetes Integration

### RBAC

The controller requires permissions for:
- Creating/updating/deleting Jobs
- Reading/writing CR status
- Reading Agents
- Reading ConfigMaps and Secrets
- Creating Events

## Documentation

### Updating Documentation

1. **Architecture changes**: Update `docs/architecture.md`
2. **API changes**: Update inline godoc comments
3. **Helm chart**: Update `charts/kubetask/README.md`
4. **Decisions**: Add ADR in `docs/adr/`

### Architecture Decision Records (ADRs)

When making significant architectural decisions:
1. Create new ADR in `docs/adr/`
2. Follow existing ADR format
3. Document context, decision, and consequences

## Git Workflow

### Commit Messages

Follow conventional commit format:

```
<type>: <description>

[optional body]

Signed-off-by: Your Name <your.email@example.com>
```

Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`

### Signing Commits

Always use signed commits:

```bash
git commit -s -m "feat: add new context type for API endpoints"
```

### Pull Requests

1. Check for upstream repositories first
2. Create PRs against upstream, not forks
3. Use descriptive titles and comprehensive descriptions
4. Reference related issues

## Troubleshooting

### Common Issues

1. **CRDs not updating**: Run `make update-crds`
2. **Deepcopy errors**: Run `make update`
3. **Lint failures**: Run `make lint` locally first
4. **E2E tests failing**: Check if Kind cluster has proper storage class

### Debugging Controllers

```bash
# Run controller with verbose logging
go run ./cmd/kubetask controller --zap-log-level=debug

# Check controller logs in cluster
kubectl logs -n kubetask-system deployment/kubetask-controller -f

# Check Job logs
kubectl logs job/<job-name> -n kubetask-system
```

## Best Practices

1. **Error Handling**: Always handle errors gracefully, log appropriately
2. **Status Updates**: Use conditions for complex status, update progress regularly
3. **Reconciliation**: Keep reconcile loops idempotent
4. **Resource Cleanup**: Use owner references for garbage collection
5. **Performance**: Avoid unnecessary API calls, use caching where appropriate
6. **Security**: Never log sensitive data (tokens, credentials)
7. **Testing**: Write tests for new features, maintain coverage

## References

- [Kubernetes Operator Pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
- [Kubebuilder Book](https://book.kubebuilder.io/)
- [Controller Runtime](https://github.com/kubernetes-sigs/controller-runtime)
- [Architecture Documentation](docs/architecture.md)

## Project Status

- **Version**: v0.1.0
- **API Stability**: v1alpha1 (subject to change)
- **License**: Apache License 2.0
- **Maintainer**: kubetask-io/kubetask team

## Getting Help

1. Review documentation in `docs/`
2. Check existing issues and PRs
3. Review Architecture Decision Records in `docs/adr/`
4. Examine existing code and tests for patterns

---

**Last Updated**: 2025-12-12
