# KubeTask Agent Developer Guide

This guide explains how to build custom agent images for KubeTask.

## Overview

KubeTask agent images are container images that execute AI-powered tasks. The architecture uses a **layered approach**:

1. **Base Image** (`kubetask-agent-base`): Universal development environment with common tools
2. **Agent Images** (gemini, goose, etc.): Extend base with specific AI CLI

This design is inspired by GitHub Actions runners and devcontainer images, providing a comprehensive development environment that covers most use cases.

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Agent Images                          │
├─────────────────┬─────────────────┬─────────────────────┤
│  gemini         │  goose          │  (your agent)       │
│  + Gemini CLI   │  + Goose CLI    │  + Your AI CLI      │
├─────────────────┴─────────────────┴─────────────────────┤
│                    Base Image                            │
│  kubetask-agent-base                                    │
│  ├── Languages: Go, Node.js, Python                    │
│  ├── Cloud CLIs: gcloud, aws, kubectl, helm            │
│  ├── Dev Tools: git, gh, make, gcc, jq, yq             │
│  └── Shell: zsh + Oh My Zsh                            │
└─────────────────────────────────────────────────────────┘
```

## Base Image Contents

The universal base image (`kubetask-agent-base`) includes:

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

### Shell Experience
- **zsh** with Oh My Zsh
- Pre-configured plugins: git, kubectl, docker, golang, npm, python, pip

## Agent Image Templates

| Template | Base | AI CLI | Use Case |
|----------|------|--------|----------|
| `gemini` | base | Gemini CLI | Google AI tasks |
| `goose` | base | Goose CLI | Multi-provider AI tasks |
| `echo` | alpine | None | E2E testing |

## Building Images

### Prerequisites

- Docker or Podman installed
- (Optional) Docker buildx for multi-arch builds

### Build Commands

From the `agents/` directory:

```bash
# Build base image first (required for other agents)
make base-build

# Build a specific agent (uses base image)
make build                    # Build gemini (default)
make AGENT=goose build        # Build goose

# Build all images at once
make build-all                # Build base + all agents

# Multi-arch build and push
make base-buildx              # Push base (linux/amd64, linux/arm64)
make AGENT=gemini buildx      # Push gemini
make buildx-all               # Push everything
```

From the project root:

```bash
# Same commands via project Makefile
make agent-base-build
make agent-build AGENT=gemini
make agent-build-all
```

### Image Naming

Default: `quay.io/zhaoxue/kubetask-agent-<name>:latest`

Customize with variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `IMG_REGISTRY` | `quay.io` | Container registry |
| `IMG_ORG` | `zhaoxue` | Registry organization |
| `VERSION` | `latest` | Image tag |

Example:
```bash
make base-build IMG_REGISTRY=docker.io IMG_ORG=myorg VERSION=v1.0.0
# Builds: docker.io/myorg/kubetask-agent-base:v1.0.0
```

## Creating a Custom Agent

### Step 1: Create Agent Directory

```bash
mkdir agents/my-agent
```

### Step 2: Create Dockerfile

```dockerfile
# My Custom Agent Image
ARG BASE_IMAGE=quay.io/zhaoxue/kubetask-agent-base:latest
FROM ${BASE_IMAGE}

# Install your AI CLI
USER root
RUN npm install -g my-ai-cli
# Or: pip install my-ai-cli
# Or: curl -fsSL https://... | bash
USER agent

# Set any required environment variables
ENV MY_AI_MODE=auto

# Define the entrypoint
ENTRYPOINT ["sh", "-c", "my-ai-cli run \"$(cat ${WORKSPACE_DIR}/task.md)\""]
```

### Step 3: Build and Test

```bash
# Build
make AGENT=my-agent build

# Test locally
echo "List files in the current directory" > /tmp/task.md
docker run --rm \
  -v /tmp/task.md:/workspace/task.md:ro \
  -e MY_API_KEY=$MY_API_KEY \
  quay.io/zhaoxue/kubetask-agent-my-agent:latest
```

## Agent Image Requirements

Every agent image must follow these conventions:

1. **Read task from `/workspace/task.md`**: The controller mounts the task description at this path
2. **Work in `/workspace` directory**: All context files are mounted here
3. **Output to stdout/stderr**: Results are captured as Job logs
4. **Exit with appropriate code**: 0 for success, non-zero for failure
5. **Run as non-root user**: The base image provides the `agent` user

## Environment Variables

### Set by Base Image

| Variable | Value | Description |
|----------|-------|-------------|
| `WORKSPACE_DIR` | `/workspace` | Workspace directory path |
| `GOPATH` | `/workspace/.go` | Go workspace |
| `GOMODCACHE` | `/workspace/.gomodcache` | Go module cache |

### Set by Controller

| Variable | Description |
|----------|-------------|
| `TASK_NAME` | Name of the Task CR |
| `TASK_NAMESPACE` | Namespace of the Task CR |

### AI Provider Credentials

Configure via the Agent `credentials` field:

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: my-agent
spec:
  agentImage: myregistry/my-agent:v1.0
  credentials:
    - name: api-key
      secretRef:
        name: ai-credentials
        key: api-key
      env: MY_API_KEY
    - name: github-token
      secretRef:
        name: github-credentials
        key: token
      env: GITHUB_TOKEN
```

## Extending the Base Image

If the base image doesn't include a tool you need, you have two options:

### Option 1: Add to Your Agent Dockerfile

```dockerfile
ARG BASE_IMAGE=quay.io/zhaoxue/kubetask-agent-base:latest
FROM ${BASE_IMAGE}

# Add additional tools
USER root
RUN apt-get update && apt-get install -y postgresql-client \
    && rm -rf /var/lib/apt/lists/*
USER agent

# Rest of your agent setup...
```

### Option 2: Contribute to Base Image

If the tool is generally useful, consider adding it to `base/Dockerfile` and submitting a PR.

## Security Best Practices

1. **Run as non-root**: Always use the `agent` user (base image handles this)
2. **Minimize additional packages**: Only install what you need in agent images
3. **Use specific versions**: Pin base image and tool versions for reproducibility
4. **Credential handling**: Never bake credentials into images; use Kubernetes secrets

## Troubleshooting

### Agent fails to start

Check that:
- The base image is available (run `make base-build` first if building locally)
- The task file is mounted at `/workspace/task.md`
- Required environment variables (API keys) are set

### Missing tools

If a tool is missing:
1. Check if it's in the base image (`docker run -it kubetask-agent-base which <tool>`)
2. Add it to your agent Dockerfile if needed
3. Consider contributing to the base image if generally useful

### Build failures

- Ensure base image is built first: `make base-build`
- Check Docker is running
- For multi-arch builds, ensure buildx is configured

## Image Size Reference

| Image | Approximate Size | Description |
|-------|-----------------|-------------|
| `base` | ~2-3 GB | Full development environment |
| `gemini` | ~2-3 GB | Base + Gemini CLI |
| `goose` | ~2-3 GB | Base + Goose CLI |
| `echo` | ~10 MB | Minimal Alpine (testing only) |

The larger size is a trade-off for having a comprehensive development environment similar to GitHub Actions runners.
