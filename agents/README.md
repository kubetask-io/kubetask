# KubeOpenCode Agent Developer Guide

This guide explains how to build custom executor images for KubeOpenCode.

## Overview

KubeOpenCode uses a **two-container pattern** for executing AI-powered tasks:

1. **OpenCode Image** (Init Container): Contains the OpenCode CLI, copies it to a shared volume
2. **Executor Image** (Worker Container): User's development environment that uses the OpenCode tool

This design separates the AI tool (OpenCode) from the execution environment, allowing users to bring their own toolsets while using a single, maintained AI agent.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Job Pod                               │
├─────────────────────────────────────────────────────────────┤
│  Init Container: opencode-init                               │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  Image: kubeopencode-agent-opencode                   │  │
│  │  - Contains OpenCode CLI (AI coding agent)            │  │
│  │  - Copies opencode binary to /tools volume            │  │
│  └───────────────────────────────────────────────────────┘  │
│                           │                                  │
│                           ▼ shared volume (/tools)           │
│                                                              │
│  Main Container: worker                                      │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  Executor Images:                                     │  │
│  │  ├── devbox        (full dev environment)             │  │
│  │  ├── code-server   (browser-based IDE)                │  │
│  │  └── user-custom   (your own toolset)                 │  │
│  │                                                       │  │
│  │  Runs: /tools/opencode run "$(cat task.md)"           │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

### Design Philosophy

| Concept | Description |
|---------|-------------|
| **Single AI Tool** | OpenCode is the only AI agent, simplifying maintenance |
| **Tool Injection** | OpenCode binary is injected via init container |
| **Custom Executors** | Users provide their own execution environments |
| **Separation of Concerns** | AI tool vs development environment are decoupled |

## Image Types

| Image | Purpose | Container Type |
|-------|---------|----------------|
| `opencode` | OpenCode CLI (AI coding agent) | Init Container |
| `devbox` | Universal development environment | Worker (Executor) |
| `code-server` | Browser-based VSCode IDE | Worker (Executor) |

## Devbox Image Contents

The universal devbox image (`kubeopencode-agent-devbox`) provides a comprehensive development environment:

### Languages & Runtimes
| Tool | Version | Description |
|------|---------|-------------|
| Go | 1.25.5 | Go programming language |
| Node.js | 22.x LTS | JavaScript runtime |
| Python | 3.x | Python interpreter + pip + venv |
| golangci-lint | latest | Go linter |

### Cloud & Kubernetes Tools
| Tool | Description |
|------|-------------|
| kubectl | Kubernetes CLI |
| helm | Kubernetes package manager |
| gcloud | Google Cloud CLI |
| aws | AWS CLI v2 |
| docker | Docker CLI (for DinD scenarios) |

### Development Tools
| Tool | Description |
|------|-------------|
| git | Version control |
| gh | GitHub CLI |
| make | Build automation |
| gcc, g++ | C/C++ compilers |
| jq | JSON processor |
| yq | YAML processor |
| vim, nano | Text editors |
| tree, htop | Utilities |

### Shell & Compatibility
- **zsh** as default shell
- **OpenShift compatible**: Works with arbitrary UIDs (uses /tmp as HOME)

## Building Images

### Prerequisites

- Docker or Podman installed
- (Optional) Docker buildx for multi-arch builds

### Build Commands

From the `agents/` directory:

```bash
# Build OpenCode image (init container)
make AGENT=opencode build

# Build devbox image (executor)
make AGENT=devbox build

# Build code-server image (executor)
make AGENT=code-server build

# Multi-arch build and push
make AGENT=opencode buildx
make AGENT=devbox buildx
```

From the project root:

```bash
# Same commands via project Makefile
make agent-build AGENT=opencode
make agent-build AGENT=devbox
```

### Image Naming

Default: `quay.io/kubeopencode/kubeopencode-agent-<name>:latest`

Customize with variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `IMG_REGISTRY` | `quay.io` | Container registry |
| `IMG_ORG` | `kubeopencode` | Registry organization |
| `VERSION` | `latest` | Image tag |

Example:
```bash
make AGENT=devbox build IMG_REGISTRY=docker.io IMG_ORG=myorg VERSION=v1.0.0
# Builds: docker.io/myorg/kubeopencode-agent-devbox:v1.0.0
```

## Creating a Custom Executor

If the default `devbox` image doesn't meet your needs, you can create a custom executor image.

### Step 1: Create Executor Directory

```bash
mkdir agents/my-executor
```

### Step 2: Create Dockerfile

```dockerfile
# My Custom Executor Image
FROM debian:bookworm-slim

# Install your tools
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    curl \
    your-custom-tools \
    && rm -rf /var/lib/apt/lists/*

# OpenShift compatibility: use /tmp as HOME for arbitrary UIDs
ENV HOME="/tmp"
ENV SHELL="/bin/bash"

# Create workspace directory
ARG WORKSPACE_DIR=/workspace
ENV WORKSPACE_DIR=${WORKSPACE_DIR}
RUN mkdir -p ${WORKSPACE_DIR} && chmod 777 ${WORKSPACE_DIR}

# Run as non-root
USER 1000:0

WORKDIR ${WORKSPACE_DIR}

CMD ["/bin/bash"]
```

### Step 3: Build and Test

```bash
# Build
make AGENT=my-executor build

# Test locally (simulating the init container pattern)
docker run --rm \
  -v /tmp/tools:/tools \
  -v /tmp/task.md:/workspace/task.md:ro \
  -e PATH="/tools:$PATH" \
  quay.io/kubeopencode/kubeopencode-agent-my-executor:latest \
  /tools/opencode run "$(cat /workspace/task.md)"
```

## Executor Image Requirements

Every executor image should follow these conventions:

1. **Work in `/workspace` directory**: All context files are mounted here
2. **Support `/tools` volume mount**: OpenCode binary is injected here
3. **Output to stdout/stderr**: Results are captured as Job logs
4. **Exit with appropriate code**: 0 for success, non-zero for failure
5. **OpenShift compatible**: Use /tmp as HOME to support arbitrary UIDs

## Environment Variables

### Set by Devbox Image

| Variable | Value | Description |
|----------|-------|-------------|
| `WORKSPACE_DIR` | `/workspace` | Workspace directory path |
| `HOME` | `/tmp` | Home directory (OpenShift compatible) |
| `GOPATH` | `/tmp/.go` | Go workspace |
| `GOMODCACHE` | `/tmp/.gomodcache` | Go module cache |

### Set by Controller

| Variable | Description |
|----------|-------------|
| `TASK_NAME` | Name of the Task CR |
| `TASK_NAMESPACE` | Namespace of the Task CR |

### AI Provider Credentials

Configure via the Agent `credentials` field:

```yaml
apiVersion: kubeopencode.io/v1alpha1
kind: Agent
metadata:
  name: my-agent
spec:
  # TBD: New fields for opencode image + executor image pattern
  credentials:
    - name: api-key
      secretRef:
        name: ai-credentials
        key: api-key
      env: ANTHROPIC_API_KEY
    - name: github-token
      secretRef:
        name: github-credentials
        key: token
      env: GITHUB_TOKEN
```

## Extending the Devbox Image

If the devbox image doesn't include a tool you need:

### Option 1: Create Custom Executor

```dockerfile
FROM quay.io/kubeopencode/kubeopencode-agent-devbox:latest

# Add additional tools
USER root
RUN apt-get update && apt-get install -y postgresql-client \
    && rm -rf /var/lib/apt/lists/*
USER 1000:0
```

### Option 2: Contribute to Devbox Image

If the tool is generally useful, consider adding it to `devbox/Dockerfile` and submitting a PR.

## Security Best Practices

1. **Run as non-root**: Use UID 1000 or support arbitrary UIDs
2. **Minimize packages**: Only install what you need
3. **Use specific versions**: Pin tool versions for reproducibility
4. **Credential handling**: Never bake credentials into images; use Kubernetes secrets
5. **OpenShift compatible**: Use /tmp for HOME and cache directories

## Troubleshooting

### Executor fails to start

Check that:
- The workspace directory is writable
- Required environment variables (API keys) are set
- The /tools volume is properly mounted

### Permission denied errors

If running on OpenShift or with arbitrary UIDs:
- Ensure HOME is set to /tmp
- Use chmod 777 for directories that need to be writable
- Don't rely on specific user IDs

### Missing tools

If a tool is missing:
1. Check if it's in the devbox image
2. Create a custom executor with the tools you need
3. Consider contributing to the devbox image if generally useful

## Image Size Reference

| Image | Approximate Size | Description |
|-------|-----------------|-------------|
| `opencode` | ~500 MB | OpenCode CLI only |
| `devbox` | ~2-3 GB | Full development environment |
| `code-server` | ~3-4 GB | Devbox + code-server |

The larger size of `devbox` is a trade-off for having a comprehensive development environment similar to GitHub Actions runners.

## Using code-server for Human-in-the-Loop

The `code-server` image provides a browser-based VSCode experience for human-in-the-loop debugging.

### Accessing code-server

After the task starts:

```bash
# Get the pod name
POD=$(kubectl get pods -l kubeopencode.io/task=my-task -o jsonpath='{.items[0].metadata.name}')

# Port forward to code-server
kubectl port-forward pod/$POD 8080:8080

# Open browser at http://localhost:8080
```

### How It Works

1. The init container injects OpenCode into /tools
2. The code-server executor runs with OpenCode available
3. Users can access VSCode in the browser to view/edit files
4. OpenCode CLI is available in the VSCode terminal for interactive AI assistance
