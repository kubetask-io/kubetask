# KubeTask

A Kubernetes-native system for executing AI-powered tasks across multiple repositories.

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Report Card](https://goreportcard.com/badge/github.com/stolostron/kubetask)](https://goreportcard.com/report/github.com/stolostron/kubetask)

## Overview

KubeTask enables you to execute AI agent tasks (like Claude) across multiple repositories in a batch manner using Kubernetes Custom Resources. It provides a declarative, GitOps-friendly approach to managing AI-powered automation workflows.

**Key Features:**

- **Kubernetes-Native**: Built on CRDs and the Operator pattern
- **Batch Processing**: Execute the same task across multiple repositories
- **AI-Agnostic**: Works with any AI agent (Claude, Gemini, etc.)
- **No External Dependencies**: Uses etcd for state, Jobs for execution
- **GitOps Ready**: Fully declarative resource definitions
- **Flexible Context System**: Support for files, repositories, and more

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
│  - Watch Batch & BatchRun CRs              │
│  - Create Kubernetes Jobs for tasks        │
│  - Update CR status                        │
└─────────────────┬───────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────┐
│         Kubernetes Jobs/Pods                │
│  - Execute tasks using AI agents           │
│  - Git operations on repositories          │
└─────────────────────────────────────────────┘
```

### Core Concepts

- **Batch**: Task template defining WHAT to do and WHERE (repositories)
- **BatchRun**: Execution instance of a Batch
- **Task**: Single task execution (simplified API)
- **Agent**: Workspace environment configuration (HOW to execute)

## Quick Start

### Prerequisites

- Kubernetes 1.25+
- Helm 3.8+
- PersistentVolume with ReadWriteMany support
- GitHub Personal Access Token
- Anthropic Claude API key or Vertex AI credentials

### Installation

```bash
# Create namespace
kubectl create namespace kubetask-system

# Install with Helm
helm install kubetask ./charts/kubetask \
  --namespace kubetask-system \
  --set github.token=<YOUR_GITHUB_TOKEN> \
  --set claude.apiKey=<YOUR_CLAUDE_API_KEY>
```

### Example Usage

#### 1. Create a Batch

Define a task to run across multiple repositories:

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Batch
metadata:
  name: update-dependencies
  namespace: kubetask-system
spec:
  # Common context - shared by all tasks
  commonContext:
    - type: File
      file:
        filePath: /workspace/task.md
        source:
          inline: |
            Update dependencies to latest versions.
            Run tests and create PR.

  # Variable context - different per task
  variableContexts:
    # Task 1: service-a
    - - type: Repository
        repository:
          org: myorg
          repo: service-a
          branch: main

    # Task 2: service-b
    - - type: Repository
        repository:
          org: myorg
          repo: service-b
          branch: main
```

#### 2. Execute the Batch

```yaml
apiVersion: kubetask.io/v1alpha1
kind: BatchRun
metadata:
  name: update-deps-001
  namespace: kubetask-system
spec:
  batchRef: update-dependencies
```

#### 3. Monitor Progress

```bash
# Watch BatchRun status
kubectl get batchrun -n kubetask-system -w

# Check detailed status
kubectl describe batchrun update-deps-001 -n kubetask-system

# View task logs
kubectl logs job/update-deps-001-task-0 -n kubetask-system
```

## Use Cases

### 1. Dependency Updates

Update dependencies across all your microservices:

```yaml
spec:
  commonContext:
    - type: File
      file:
        filePath: /workspace/task.md
        source:
          inline: "Update go.mod to Go 1.22 and run go mod tidy"
  variableContexts:
    - [{type: Repository, repository: {org: myorg, repo: service-a, branch: main}}]
    - [{type: Repository, repository: {org: myorg, repo: service-b, branch: main}}]
```

### 2. Security Patches

Apply security fixes across repositories:

```yaml
spec:
  commonContext:
    - type: File
      file:
        filePath: /workspace/task.md
        source:
          inline: "Fix CVE-2024-XXXXX by updating package X to version Y"
  variableContexts:
    # List all affected repositories
```

### 3. Code Refactoring

Perform consistent refactoring across codebases:

```yaml
spec:
  commonContext:
    - type: File
      file:
        filePath: /workspace/task.md
        source:
          inline: "Rename function oldName to newName across the codebase"
```

### 4. Documentation Updates

Update documentation across multiple projects:

```yaml
spec:
  commonContext:
    - type: File
      file:
        filePath: /workspace/task.md
        source:
          inline: "Update README with new installation instructions"
```

## Key Features

### Flexible Context System

KubeTask supports multiple context types:

- **File Context**: Task descriptions, configurations
  - Inline content
  - From ConfigMap
  - From Secret
  - **Path-based aggregation**: Multiple contexts with the same `filePath` are aggregated into a single file
- **Repository Context**: GitHub repositories
- **Extensible**: Easy to add new context types (API, Database, etc.)

### Agent and Agent Image

KubeTask uses Agent to define the agent container image:

1. **Batch/Task** references a Agent via `agentRef`
2. **Default**: If not specified, uses Agent named "default"
3. **Fallback**: If no Agent found, uses built-in default image

```yaml
# Create Agent
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: default
  namespace: kubetask-system
spec:
  agentImage: quay.io/myorg/claude-agent:v1.0
```

### Multi-AI Support

Use different Agents for different AI agents:

```bash
# Create Agents for different agents
kubectl apply -f - <<EOF
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: claude-workspace
spec:
  agentImage: quay.io/myorg/claude-agent:v1.0
---
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: gemini-workspace
spec:
  agentImage: quay.io/myorg/gemini-agent:v1.0
EOF

# Create Batch with specific workspace
kubectl apply -f - <<EOF
apiVersion: kubetask.io/v1alpha1
kind: Batch
metadata:
  name: update-with-gemini
spec:
  agentRef: gemini-workspace
  commonContext:
    - type: File
      file:
        filePath: /workspace/task.md
        source:
          inline: "Update dependencies"
  variableContexts:
    - [{type: Repository, repository: {org: myorg, repo: service-a, branch: main}}]
EOF
```

## Development

### Building from Source

```bash
# Clone the repository
git clone https://github.com/stolostron/kubetask.git
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
    repository: quay.io/zhaoxue/kubetask-controller
    tag: v0.1.0
  resources:
    limits:
      cpu: 500m
      memory: 512Mi

workspace:
  enabled: true
  size: 100Gi
  storageClass: ""  # Uses default storage class

github:
  token: ""  # Your GitHub token

claude:
  apiKey: ""  # Your Claude API key
  # Or use Vertex AI
  vertexAI:
    enabled: false
    projectID: ""
    location: ""

cleanup:
  enabled: true
  schedule: "0 2 * * *"
  ttlDays: 3
```

## Documentation

- [Architecture](docs/architecture.md) - Detailed architecture and design decisions
- [Helm Chart](charts/kubetask/README.md) - Deployment and configuration guide
- [ADRs](docs/adr/) - Architecture Decision Records

## Storage Requirements

KubeTask requires a PersistentVolume with `ReadWriteMany` access mode for concurrent agent execution.

**Supported storage solutions:**
- NFS
- CephFS
- Azure Files
- Google Cloud Filestore
- AWS EFS

## Security

### RBAC

KubeTask follows the principle of least privilege:

- **Controller**: Manages CRs and Jobs only
- **Agent**: Updates BatchRun status only

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
kubectl auth can-i create batchruns \
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

### Storage Issues

```bash
# Verify PVC is bound
kubectl get pvc -n kubetask-system

# Check if storage class supports ReadWriteMany
kubectl describe pvc kubetask-workspace -n kubetask-system
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

- [ ] Enhanced status reporting and observability
- [ ] Support for additional context types (API, Database)
- [ ] Advanced retry and failure handling
- [ ] Integration with more AI providers
- [ ] Web UI for monitoring and management
- [ ] GitOps integration examples (Flux, ArgoCD)

## Community

- **Issues**: [GitHub Issues](https://github.com/stolostron/kubetask/issues)
- **Discussions**: [GitHub Discussions](https://github.com/stolostron/kubetask/discussions)

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

Made with ❤️ by the KubeTask community
