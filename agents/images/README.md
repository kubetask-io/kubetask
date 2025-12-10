# KubeTask Agent Images

This directory contains Dockerfiles for AI agent images used by KubeTask.

## Available Agents

| Agent | Description | Base Image |
|-------|-------------|------------|
| `gemini` | Google Gemini CLI agent | `node:24-slim` |
| `echo` | Echo agent for testing | `alpine` |

## Building Agent Images

### Using Make (Recommended)

From the project root directory:

```bash
# Build specific agent (default: gemini)
make agent-build                    # Build gemini agent
make agent-build AGENT=gemini       # Build gemini agent (explicit)
make agent-build AGENT=echo         # Build echo agent

# Push agent image to registry
make agent-push                     # Push gemini agent
make agent-push AGENT=claude        # Push claude agent

# Multi-arch build and push (linux/amd64 and linux/arm64)
make agent-buildx                   # Multi-arch build gemini
make agent-buildx AGENT=claude      # Multi-arch build claude

# List available agents
make -C agents/images list
```

### From This Directory

```bash
cd agents/images

# Build gemini agent
make build

# Build specific agent
make AGENT=echo build

# Push to registry
make push

# Multi-arch build and push
make buildx

# List available agents
make list
```

## Image Configuration

Default image naming: `quay.io/zhaoxue/kubetask-agent-<AGENT>:latest`

Customize with environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `AGENT` | `gemini` | Agent to build |
| `IMG_REGISTRY` | `quay.io` | Container registry |
| `IMG_ORG` | `zhaoxue` | Registry organization |
| `VERSION` | `latest` | Image tag version |

Example:

```bash
make agent-build AGENT=gemini IMG_REGISTRY=docker.io IMG_ORG=myorg VERSION=v1.0.0
# Builds: docker.io/myorg/kubetask-agent-gemini:v1.0.0
```

## Agent Requirements

Each agent image should:

1. Read task description from `/workspace/task.md`
2. Execute the task in the `/workspace` directory
3. Output results to stdout/stderr for logging

## Adding a New Agent

1. Create a new directory: `mkdir <agent-name>`
2. Add a `Dockerfile` with the agent setup
3. Ensure the entrypoint reads from `/workspace/task.md`
4. Test locally: `make AGENT=<agent-name> build`

## Installed Tools

### Gemini Agent

The Gemini agent includes the following tools:

- `git` - Version control
- `jq` - JSON processing
- `yq` - YAML processing
- `gh` - GitHub CLI
- `gemini` - Google Gemini CLI

### Echo Agent

Minimal Alpine-based image for testing:

- `sh` - Shell
- Basic Unix utilities
