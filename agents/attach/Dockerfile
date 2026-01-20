# KubeOpenCode Lightweight Attach Image
#
# This minimal image is used for Server-mode --attach Pods.
# When a Task references a Server-mode Agent, the Task controller creates
# a Pod that runs `opencode run --attach <server-url> "task"`. This Pod only
# needs the OpenCode binary and network access - no development tools required.
#
# The actual task execution happens in the persistent OpenCode server's
# environment (which uses the full devbox image). This Pod just sends the
# task to the server and streams the output.
#
# Size comparison:
# - devbox: ~1GB (Go, Node, Python, cloud CLIs, dev tools)
# - attach: ~150MB (Debian slim + OpenCode binary)
#
FROM debian:bookworm-slim

# CA certificates for HTTPS connections to OpenCode server and AI APIs
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy OpenCode binary from the opencode agent image
# Both images use Debian, so glibc binary is compatible
COPY --from=quay.io/kubeopencode/kubeopencode-agent-opencode:latest /opencode /usr/local/bin/opencode

# Create workspace directory for task.md mount
# This matches the pattern used in devbox and other executor images
ARG WORKSPACE_DIR=/workspace
ENV WORKSPACE_DIR=${WORKSPACE_DIR}
RUN mkdir -p ${WORKSPACE_DIR} && chmod 777 ${WORKSPACE_DIR}

# Create /tools directory to match the init container pattern
# Even though we have opencode in /usr/local/bin, the command uses /tools/opencode
RUN mkdir -p /tools && chmod 777 /tools

# Non-root user for security (OpenShift compatible)
# Using UID 1000 with GID 0 (root group) for OpenShift compatibility
USER 1000:0

WORKDIR ${WORKSPACE_DIR}

# Default entrypoint - usually overridden by Pod command
ENTRYPOINT ["/usr/local/bin/opencode"]
