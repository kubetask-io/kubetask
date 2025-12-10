# KubeTask Tools Image
#
# This image provides CLI tools for AI agents to use during task execution.
# Tools are organized under /tools directory following the standard structure:
#   /tools/bin/  - Executables (added to PATH)
#   /tools/lib/  - Shared libraries and runtimes
#
# The controller copies /tools to a shared volume via initContainer,
# making all tools available to the agent container.

FROM node:22-slim AS builder

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Create tools directory structure
RUN mkdir -p /tools/bin /tools/lib

# Download kubectl (static binary)
ARG KUBECTL_VERSION=v1.31.0
RUN curl -LO "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" \
    && chmod +x kubectl \
    && mv kubectl /tools/bin/

# Download gh CLI (static binary)
ARG GH_VERSION=2.62.0
RUN curl -L "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_linux_amd64.tar.gz" \
    | tar xz \
    && mv gh_${GH_VERSION}_linux_amd64/bin/gh /tools/bin/ \
    && rm -rf gh_${GH_VERSION}_linux_amd64

# Download jq (static binary)
ARG JQ_VERSION=1.7.1
RUN curl -L "https://github.com/jqlang/jq/releases/download/jq-${JQ_VERSION}/jq-linux-amd64" \
    -o /tools/bin/jq \
    && chmod +x /tools/bin/jq

# Copy node runtime for Node.js based tools
RUN cp /usr/local/bin/node /tools/bin/

# Install Node.js CLI tools globally and copy to /tools/lib
# Note: Add your Node.js CLI tools here
# RUN npm install -g @anthropic-ai/claude-code \
#     && cp -r /usr/local/lib/node_modules /tools/lib/

# Final stage - minimal image with just /tools
FROM scratch AS final

COPY --from=builder /tools /tools

# Default command does nothing - this image is used as an initContainer source
CMD ["/bin/true"]

# For debugging, you can build with the builder stage:
# docker build --target builder -t kubetask-tools:debug .
