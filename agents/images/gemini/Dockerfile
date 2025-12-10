# Gemini CLI Agent Image
FROM node:24-slim

# Install git, jq, gh, yq, kubectl, make, procps, gcc and other common tools needed for repository operations
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    jq \
    ca-certificates \
    curl \
    gpg \
    procps \
    make \
    gcc \
    libc6-dev \
    && mkdir -p /etc/apt/keyrings \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | gpg --dearmor -o /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends gh \
    && curl -fsSL https://github.com/mikefarah/yq/releases/latest/download/yq_linux_$(dpkg --print-architecture) -o /usr/local/bin/yq \
    && chmod +x /usr/local/bin/yq \
    && curl -fsSL "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/$(dpkg --print-architecture | sed 's/aarch64/arm64/' | sed 's/x86_64/amd64/')/kubectl" -o /usr/local/bin/kubectl \
    && chmod +x /usr/local/bin/kubectl \
    && apt-get purge -y gpg \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/*

# Install Go
ARG GO_VERSION=1.25.5
RUN ARCH=$(dpkg --print-architecture | sed 's/amd64/amd64/' | sed 's/arm64/arm64/') \
    && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" -o /tmp/go.tar.gz \
    && tar -C /usr/local -xzf /tmp/go.tar.gz \
    && rm /tmp/go.tar.gz

# Install Gemini CLI globally
RUN npm install -g @google/gemini-cli

# Install golangci-lint
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b /usr/local/bin

# Build argument for workspace directory
ARG WORKSPACE_DIR=/workspace

# Set workspace directory as environment variable for runtime use
ENV WORKSPACE_DIR=${WORKSPACE_DIR}

# Set Go environment - use workspace-relative paths to avoid permission issues
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="${WORKSPACE_DIR}/.go"
ENV GOMODCACHE="${WORKSPACE_DIR}/.gomodcache"
ENV PATH="${GOPATH}/bin:${PATH}"

# Create non-root user for security
RUN useradd -m -s /bin/bash gemini
USER gemini
WORKDIR ${WORKSPACE_DIR}

# Default entrypoint - run Gemini CLI in headless mode
# The agent reads the task from ${WORKSPACE_DIR}/task.md and executes it
# Using -p flag for direct prompt input from the mounted context file
# Using --yolo to auto-approve actions (safe in container environment)
# Using stream-json output to see all logs
ENTRYPOINT ["sh", "-c", "gemini --output-format stream-json --yolo -p \"$(cat ${WORKSPACE_DIR}/task.md)\""]
