# Tools Image Specification

This document defines the specification for building tools images that can be used with KubeTask.

## Overview

Tools images provide CLI tools (git, gh, kubectl, etc.) for AI agents to use during task execution. They are decoupled from agent images, allowing:

- Independent versioning of tools and agents
- Runtime composition via WorkspaceConfig
- Reuse of tools across different agent types

## Directory Structure

Tools images must organize their contents under `/tools`:

```
/tools/
├── bin/              # Executables (added to PATH)
│   ├── git
│   ├── gh
│   ├── kubectl
│   ├── jq
│   └── node          # Required for Node.js CLI tools
├── lib/              # Shared libraries and runtimes
│   ├── node_modules/ # Node.js dependencies (for npm-installed CLIs)
│   └── *.so          # Dynamic libraries if needed
└── etc/              # Optional: tool configurations
```

## How It Works

1. **WorkspaceConfig specifies toolsImage**:
   ```yaml
   apiVersion: kubetask.io/v1alpha1
   kind: WorkspaceConfig
   metadata:
     name: my-workspace
   spec:
     agentImage: quay.io/zhaoxue/kubetask-agent-gemini:latest
     toolsImage: quay.io/zhaoxue/kubetask-tools:latest
   ```

2. **Controller generates Job with initContainer**:
   ```yaml
   initContainers:
   - name: copy-tools
     image: quay.io/zhaoxue/kubetask-tools:latest
     command: ["sh", "-c", "cp -a /tools/. /shared-tools/"]
     volumeMounts:
     - name: tools-volume
       mountPath: /shared-tools
   ```

3. **Agent container has tools available**:
   ```yaml
   containers:
   - name: agent
     env:
     - name: PATH
       value: "/tools/bin:/usr/local/bin:/usr/bin:/bin"
     - name: NODE_PATH
       value: "/tools/lib/node_modules"
     volumeMounts:
     - name: tools-volume
       mountPath: /tools
   ```

## Building a Tools Image

### Basic Example (Static Binaries)

```dockerfile
FROM alpine:3.19 AS builder

RUN apk add --no-cache curl

# Create directory structure
RUN mkdir -p /tools/bin /tools/lib

# Download kubectl
RUN curl -LO "https://dl.k8s.io/release/v1.31.0/bin/linux/amd64/kubectl" \
    && chmod +x kubectl && mv kubectl /tools/bin/

# Download gh CLI
RUN curl -L "https://github.com/cli/cli/releases/download/v2.62.0/gh_2.62.0_linux_amd64.tar.gz" \
    | tar xz && mv gh_*/bin/gh /tools/bin/

FROM scratch
COPY --from=builder /tools /tools
```

### With Node.js CLI Tools

```dockerfile
FROM node:22-slim AS builder

RUN mkdir -p /tools/bin /tools/lib

# Copy node runtime
RUN cp /usr/local/bin/node /tools/bin/

# Install Node.js CLI tools
RUN npm install -g @anthropic-ai/claude-code
RUN cp -r /usr/local/lib/node_modules /tools/lib/

# Create wrapper scripts for Node.js CLIs
RUN ln -s ../lib/node_modules/.bin/claude /tools/bin/claude

FROM scratch
COPY --from=builder /tools /tools
```

### With Git and Other System Tools

For tools with dynamic library dependencies:

```dockerfile
FROM ubuntu:24.04 AS builder

RUN apt-get update && apt-get install -y git curl jq

RUN mkdir -p /tools/bin /tools/lib

# Copy binaries
RUN cp /usr/bin/git /tools/bin/
RUN cp /usr/bin/jq /tools/bin/

# Copy required libraries (check with ldd)
RUN ldd /usr/bin/git | grep "=>" | awk '{print $3}' | xargs -I{} cp {} /tools/lib/ 2>/dev/null || true

FROM scratch
COPY --from=builder /tools /tools
```

## Environment Variables

The controller sets these environment variables in the agent container when `toolsImage` is specified:

| Variable | Value | Purpose |
|----------|-------|---------|
| `PATH` | `/tools/bin:/usr/local/bin:/usr/bin:/bin` | Include tools binaries |
| `NODE_PATH` | `/tools/lib/node_modules` | Node.js module resolution |
| `LD_LIBRARY_PATH` | `/tools/lib` | Dynamic library loading |

## Best Practices

1. **Use multi-stage builds** to minimize image size
2. **Prefer static binaries** when available (kubectl, gh, jq)
3. **Include node runtime** if using any npm-installed CLI tools
4. **Test locally** by running the tools image:
   ```bash
   docker run --rm -it --entrypoint sh kubetask-tools:latest
   ls -la /tools/bin/
   ```

## Versioning

Use semantic versioning for tools images:

```
quay.io/zhaoxue/kubetask-tools:v1.0.0
quay.io/zhaoxue/kubetask-tools:v1.0.0-kubectl1.31-gh2.62
```

## Example WorkspaceConfig

```yaml
apiVersion: kubetask.io/v1alpha1
kind: WorkspaceConfig
metadata:
  name: gemini-with-devtools
spec:
  agentImage: quay.io/zhaoxue/kubetask-agent-gemini:latest
  toolsImage: quay.io/zhaoxue/kubetask-tools:v1.0.0
  defaultContexts:
    - type: File
      file:
        name: coding-standards.md
        source:
          configMapKeyRef:
            name: org-standards
            key: coding.md
```
