# Claude Development Guidelines for KubeTask

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

## Core Concepts

### Resource Hierarchy

1. **Task** - Single task execution (the primary API)
2. **CronTask** - Scheduled/recurring task execution (creates Tasks on cron schedule)
3. **Agent** - AI agent configuration (HOW to execute)

### Important Design Decisions

- **Agent** (not KubeTaskConfig) - Stable, project-independent naming
- **AgentImage** (not AgentTemplateRef) - Simple container image, controller generates Jobs
- **agentRef** - Reference from Task to Agent
- **No Batch/BatchRun** - Use Helm/Kustomize to create multiple Tasks (Kubernetes-native approach)

### Context System

Tasks operate on different types of contexts:
- **File Context**: Task descriptions, configuration files (from inline or ConfigMap)
  - `FilePath` + `Inline`: Single file with inline content
  - `FilePath` + `ConfigMapKeyRef`: Single file from ConfigMap key
  - `DirPath` + `ConfigMapRef`: Directory with all ConfigMap keys as files
- **Future**: MCP contexts (extensible design)

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
   - Kinds: `Task`, `CronTask`, `Agent`

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

```bash
# Setup complete e2e environment
make e2e-setup

# Run e2e tests
make e2e-test

# Teardown e2e environment
make e2e-teardown

# For iterative development
make e2e-reload  # Rebuild and reload controller image
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

The agent images are tagged as `quay.io/zhaoxue/kubetask-agent-<AGENT>:latest` by default. You can customize the registry, org, and version:

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
│   ├── types.go           # Main API types (Task, CronTask, Agent)
│   ├── register.go        # Scheme registration
│   └── zz_generated.deepcopy.go  # Generated deepcopy
├── cmd/controller/        # Controller main entry point
│   └── main.go
├── internal/controller/   # Controller reconcilers
│   ├── task_controller.go
│   └── crontask_controller.go
├── deploy/               # Kubernetes manifests
│   └── crds/            # Generated CRD YAMLs (Task, CronTask, Agent)
├── charts/kubetask/     # Helm chart
├── hack/                # Build and codegen scripts
├── docs/                # Documentation
│   ├── architecture.md  # Architecture documentation
│   └── adr/             # Architecture Decision Records
└── Makefile             # Build automation
```

## Making Changes

### Adding New API Fields

1. Update `api/v1alpha1/types.go`
2. Add appropriate kubebuilder markers
3. Run `make update` to regenerate CRDs and deepcopy
4. Run `make verify` to ensure everything is correct
5. Update documentation in `docs/architecture.md`

### Modifying Controllers

1. Update controller logic in `internal/controller/`
2. Ensure proper error handling and logging
3. Update status conditions appropriately
4. Test locally with `make run` or `make e2e-setup`

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

### Agent Image Discovery

The agent image is discovered via:
1. `Agent.spec.agentImage` (from referenced Agent)
2. Built-in default image (fallback: `quay.io/zhaoxue/kubetask-agent-gemini:latest`)

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
go run ./cmd/controller/main.go --zap-log-level=debug

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
- **Maintainer**: stolostron/kubetask team

## Getting Help

1. Review documentation in `docs/`
2. Check existing issues and PRs
3. Review Architecture Decision Records in `docs/adr/`
4. Examine existing code and tests for patterns

---

**Last Updated**: 2025-12-10
