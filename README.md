<p align="center">
  <img src="docs/images/logo.png" alt="KubeTask Logo" width="400">
</p>

<h1 align="center">KubeTask</h1>

<p align="center">
  A Kubernetes-native system for executing AI-powered tasks.
</p>

<p align="center">
  <a href="https://opensource.org/licenses/Apache-2.0"><img src="https://img.shields.io/badge/License-Apache%202.0-blue.svg" alt="License"></a>
  <a href="https://goreportcard.com/report/github.com/kubetask-io/kubetask"><img src="https://goreportcard.com/badge/github.com/kubetask-io/kubetask" alt="Go Report Card"></a>
</p>

## Overview

KubeTask enables you to execute AI agent tasks (like Claude, Gemini) using Kubernetes Custom Resources. It provides a simple, declarative, GitOps-friendly approach to running AI agents as Kubernetes Jobs.

**Key Features:**

- **Kubernetes-Native**: Built on CRDs and the Operator pattern
- **Simple API**: Core CRDs - Task, Workflow, WorkflowRun, CronWorkflow, Agent, and Context
- **AI-Agnostic**: Works with any AI agent (Claude, Gemini, Goose, etc.)
- **No External Dependencies**: Uses etcd for state, Jobs for execution
- **GitOps Ready**: Fully declarative resource definitions
- **Flexible Context System**: Support for inline content, ConfigMaps, and Git repositories
- **Scheduled Workflows**: CronWorkflow for recurring AI-powered operations
- **Batch Operations**: Use Helm/Kustomize for multiple Tasks (Kubernetes-native approach)

## Architecture

```
┌─────────────────────────────────────────────┐
│         Kubernetes API Server               │
│  - Custom Resource Definitions (CRDs)       │
│  - RBAC & Authentication                    │
└─────────────────┬───────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────┐
│      KubeTask Controller (Operator)         │
│  - Watch Task CRs                           │
│  - Create Kubernetes Jobs for tasks         │
│  - Update CR status                         │
└─────────────────┬───────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────┐
│         Kubernetes Jobs/Pods                │
│  - Execute tasks using AI agents            │
│  - Context files mounted as volumes         │
└─────────────────────────────────────────────┘
```

### Core Concepts

- **Task**: Single task execution (the primary API)
- **Workflow**: Reusable multi-stage task template
- **WorkflowRun**: Workflow execution instance
- **CronWorkflow**: Scheduled WorkflowRun triggering
- **Agent**: AI agent configuration (HOW to execute)
- **Context**: Reusable context resources (inline, ConfigMap, or Git)

## Quick Start

### Prerequisites

- Kubernetes 1.25+
- Helm 3.8+

### Installation

```bash
# Create namespace
kubectl create namespace kubetask-system

# Install from OCI registry
helm install kubetask oci://quay.io/kubetask/kubetask \
  --namespace kubetask-system

# Or install from local chart (for development)
helm install kubetask ./charts/kubetask \
  --namespace kubetask-system
```

### Example Usage

#### 1. Create an Agent

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: default
  namespace: kubetask-system
spec:
  agentImage: quay.io/kubetask/kubetask-agent-gemini:latest
  workspaceDir: /workspace  # Optional, defaults to /workspace
  serviceAccountName: kubetask-agent
  credentials:
    - name: gemini-api-key
      secretRef:
        name: ai-credentials
        key: gemini-key
      env: GEMINI_API_KEY
```

#### 2. Create a Context (Optional)

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Context
metadata:
  name: process-guide
  namespace: kubetask-system
spec:
  type: ConfigMap
  configMap:
    name: process-guides
    key: pr-process.md
```

#### 3. Create a Task

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: update-service-a
  namespace: kubetask-system
spec:
  # Task description (becomes /workspace/task.md)
  description: |
    Update dependencies to latest versions.
    Run tests and create PR.

  # Reference reusable Context CRDs
  contexts:
    - name: process-guide
      mountPath: /workspace/guide.md
```

#### 4. Monitor Progress

```bash
# Watch Task status
kubectl get tasks -n kubetask-system -w

# Check detailed status
kubectl describe task update-service-a -n kubetask-system

# View task logs
kubectl logs job/$(kubectl get task update-service-a -o jsonpath='{.status.jobName}') -n kubetask-system
```

### Batch Operations with Helm

For running the same task across multiple targets, use Helm templating:

```yaml
# values.yaml
tasks:
  - name: update-service-a
    repo: service-a
  - name: update-service-b
    repo: service-b
  - name: update-service-c
    repo: service-c

# templates/tasks.yaml
{{- range .Values.tasks }}
---
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: {{ .name }}
spec:
  description: "Update dependencies for {{ .repo }}"
{{- end }}
```

```bash
# Generate and apply multiple tasks
helm template my-tasks ./chart | kubectl apply -f -
```

## Key Features

### Flexible Context System

KubeTask uses a **Context CRD** to provide reusable context to AI agents:

- **Inline Content**:
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
        - Use descriptive names
        - Write unit tests
  ```

- **ConfigMap Reference**:
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

- **Git Repository**:
  ```yaml
  apiVersion: kubetask.io/v1alpha1
  kind: Context
  metadata:
    name: repo-context
  spec:
    type: Git
    git:
      repository: https://github.com/org/contexts
      path: .claude/
      ref: main
      secretRef:
        name: git-credentials  # Optional, for private repos
  ```

Tasks reference Contexts using **ContextMount**:
```yaml
spec:
  description: "Review the code"
  contexts:
    - name: coding-standards
      mountPath: /workspace/guides/standards.md
    - name: security-policy
      # Empty mountPath = append to task.md with XML tags
```

- **Content Aggregation**: Contexts without `mountPath` are aggregated into `/workspace/task.md` with XML tags

### Agent Configuration

Agent centralizes execution environment configuration:

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: default
spec:
  agentImage: quay.io/kubetask/kubetask-agent-gemini:latest
  workspaceDir: /workspace  # Configurable workspace directory
  serviceAccountName: kubetask-agent

  # Default contexts for all tasks (references to Context CRDs)
  contexts:
    - name: org-coding-standards
      # Empty mountPath = append to task.md with XML tags
    - name: org-security-policy

  # Credentials (secrets as env vars or file mounts)
  credentials:
    - name: github-token
      secretRef:
        name: github-creds
        key: token
      env: GITHUB_TOKEN

    - name: ssh-key
      secretRef:
        name: ssh-keys
        key: id_rsa
      mountPath: /home/agent/.ssh/id_rsa
      fileMode: 0400

  # Pod scheduling
  podSpec:
    scheduling:
      nodeSelector:
        workload-type: ai-agent
```

### Multi-AI Support

Use different Agents for different AI agents:

```yaml
# Claude agent
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: claude
spec:
  agentImage: quay.io/kubetask/kubetask-agent-claude:latest
  serviceAccountName: kubetask-agent
---
# Gemini agent
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: gemini
spec:
  agentImage: quay.io/kubetask/kubetask-agent-gemini:latest
  serviceAccountName: kubetask-agent
---
# Task using specific agent
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: task-with-claude
spec:
  agentRef: claude
  description: "Update dependencies and create a PR"
```

## Agent Images

KubeTask provides **template agent images** that serve as starting points for building your own customized agents. These templates demonstrate the agent interface pattern and include common development tools, but are designed to be customized based on your specific requirements.

**Important**: The provided agent images (gemini, goose, echo) are examples/templates. You should build and customize your own agent images according to your needs:

- Choose which AI CLI to include (Gemini, Claude Code, Goose, etc.)
- Install the specific tools your tasks require
- Configure credentials and environment variables
- Optimize image size for your use case

### Available Templates

| Template | Description | Use Case |
|----------|-------------|----------|
| `gemini` | Google Gemini CLI with Go, git, kubectl | General development tasks |
| `claude` | Anthropic Claude Code CLI with Go, git, kubectl | Claude-powered tasks |
| `goose` | Block's Goose agent with Go, git, kubectl | Multi-provider AI tasks |
| `echo` | Minimal Alpine image | E2E testing and debugging |

### Building Your Agent

```bash
# Build from template
make agent-build AGENT=gemini

# Customize registry and version
make agent-build AGENT=gemini IMG_REGISTRY=docker.io IMG_ORG=myorg VERSION=v1.0.0
```

For detailed guidance on building custom agent images, see the [Agent Developer Guide](agents/README.md).

## Development

### Building from Source

```bash
# Clone the repository
git clone https://github.com/kubetask-io/kubetask.git
cd kubetask

# Build the controller
make build

# Run tests
make test

# Run linter
make lint
```

### Local Development

```bash
# Run controller locally (requires kubeconfig)
make run

# Format code
make fmt

# Update generated code
make update
```

### E2E Testing

```bash
# Setup e2e environment
make e2e-setup

# Run e2e tests
make e2e-test

# Teardown e2e environment
make e2e-teardown
```

### Docker Images

```bash
# Build docker image
make docker-build

# Build and push multi-arch images
make docker-buildx
```

## Configuration

### Helm Chart Values

See [charts/kubetask/README.md](charts/kubetask/README.md) for complete configuration options.

Key configurations:

```yaml
controller:
  image:
    repository: quay.io/kubetask/kubetask-controller
    tag: v0.1.0
  resources:
    limits:
      cpu: 500m
      memory: 512Mi
```

## Documentation

- [Architecture](docs/architecture.md) - Detailed architecture and design decisions
- [Agent Context Spec](docs/agent-context-spec.md) - How contexts are mounted
- [Helm Chart](charts/kubetask/README.md) - Deployment and configuration guide
- [ADRs](docs/adr/) - Architecture Decision Records

## Security

### RBAC

KubeTask follows the principle of least privilege:

- **Controller**: Manages CRs and Jobs only

### Secrets Management

Never commit secrets to Git. Use:
- Kubernetes Secrets
- External Secrets Operator
- Sealed Secrets
- HashiCorp Vault

### Pod Security

- Runs with non-root user
- Dropped capabilities
- Read-only root filesystem (where applicable)

## Troubleshooting

### Controller Issues

```bash
# Check controller logs
kubectl logs -n kubetask-system deployment/kubetask-controller

# Verify RBAC
kubectl auth can-i create tasks \
  --as=system:serviceaccount:kubetask-system:kubetask-controller
```

### Job Failures

```bash
# List failed jobs
kubectl get jobs -n kubetask-system --field-selector status.successful=0

# Check job logs
kubectl logs job/<job-name> -n kubetask-system

# Describe job for events
kubectl describe job/<job-name> -n kubetask-system
```

## Contributing

We welcome contributions! Please follow these guidelines:

1. **Commit Standards**: Use signed commits with `-s` flag
   ```bash
   git commit -s -m "feat: add new feature"
   ```

2. **Pull Requests**:
   - Check for upstream repositories first
   - Create PRs against upstream, not forks
   - Use descriptive titles and comprehensive descriptions

3. **Code Standards**:
   - Write code comments in English
   - Follow Go conventions
   - Run `make lint` before submitting

4. **Testing**:
   - Write tests for new features
   - Ensure `make test` passes
   - Test e2e changes with `make e2e-test`

See [CLAUDE.md](CLAUDE.md) for detailed development guidelines.

## Roadmap

- [x] Workflow and WorkflowRun for multi-stage task execution
- [x] CronWorkflow for scheduled workflow execution
- [x] Context CRD for reusable contexts
- [x] GitContext for Git repository support
- [ ] Enhanced status reporting and observability
- [ ] Support for additional context types (MCP)
- [ ] Advanced retry and failure handling
- [ ] Integration with more AI providers
- [ ] Web UI for monitoring and management
- [ ] GitOps integration examples (Flux, ArgoCD)

## Community

- **Issues**: [GitHub Issues](https://github.com/kubetask-io/kubetask/issues)
- **Discussions**: [GitHub Discussions](https://github.com/kubetask-io/kubetask/discussions)

## License

Copyright Contributors to the KubeTask project.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the License. You may obtain a copy of the License at:

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific language governing permissions and limitations under the License.

## Acknowledgments

KubeTask is inspired by:
- Tekton Pipelines
- Argo Workflows
- Kubernetes Batch API

Built with:
- [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime)
- [kubebuilder](https://github.com/kubernetes-sigs/kubebuilder)

---

Made with love by the KubeTask community
