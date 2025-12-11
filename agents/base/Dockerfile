# KubeTask Universal Agent Base Image
#
# This base image provides a comprehensive development environment similar to
# GitHub Actions runners and devcontainer images. It includes:
# - Multi-language runtimes: Go, Node.js, Python
# - Cloud CLIs: gcloud, aws, kubectl, helm
# - Development tools: git, gh, make, gcc, jq, yq
# - Shell: zsh with Oh My Zsh
#
# AI agent images (gemini, goose, etc.) build on top of this base.
#
FROM debian:bookworm-slim

# === Core Utilities ===
# Essential tools for development and automation
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    jq \
    ca-certificates \
    curl \
    wget \
    gnupg \
    procps \
    make \
    gcc \
    g++ \
    libc6-dev \
    openssh-client \
    unzip \
    zip \
    bzip2 \
    xz-utils \
    tree \
    htop \
    vim \
    nano \
    less \
    && rm -rf /var/lib/apt/lists/*

# === GitHub CLI ===
RUN mkdir -p /etc/apt/keyrings \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | \
       gpg --dearmor -o /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
       > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/*

# === yq (YAML processor) ===
RUN curl -fsSL "https://github.com/mikefarah/yq/releases/latest/download/yq_linux_$(dpkg --print-architecture)" \
    -o /usr/local/bin/yq && chmod +x /usr/local/bin/yq

# === kubectl ===
RUN ARCH=$(dpkg --print-architecture | sed 's/aarch64/arm64/' | sed 's/x86_64/amd64/') \
    && curl -fsSL "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/${ARCH}/kubectl" \
    -o /usr/local/bin/kubectl && chmod +x /usr/local/bin/kubectl

# === helm ===
RUN curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash

# === Go ===
ARG GO_VERSION=1.25.5
RUN ARCH=$(dpkg --print-architecture) \
    && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" -o /tmp/go.tar.gz \
    && tar -C /usr/local -xzf /tmp/go.tar.gz \
    && rm /tmp/go.tar.gz
ENV PATH="/usr/local/go/bin:${PATH}"

# === golangci-lint ===
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b /usr/local/bin

# === Node.js (LTS) ===
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/* \
    && npm install -g npm@latest

# === Python 3 ===
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 \
    python3-pip \
    python3-venv \
    && rm -rf /var/lib/apt/lists/* \
    && ln -sf /usr/bin/python3 /usr/bin/python

# === Google Cloud CLI ===
RUN curl -fsSL https://packages.cloud.google.com/apt/doc/apt-key.gpg | \
    gpg --dearmor -o /etc/apt/keyrings/google-cloud.gpg \
    && echo "deb [signed-by=/etc/apt/keyrings/google-cloud.gpg] https://packages.cloud.google.com/apt cloud-sdk main" \
       > /etc/apt/sources.list.d/google-cloud-sdk.list \
    && apt-get update && apt-get install -y --no-install-recommends google-cloud-cli \
    && rm -rf /var/lib/apt/lists/*

# === AWS CLI v2 ===
RUN ARCH=$(dpkg --print-architecture | sed 's/amd64/x86_64/' | sed 's/arm64/aarch64/') \
    && curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-${ARCH}.zip" -o /tmp/awscliv2.zip \
    && unzip -q /tmp/awscliv2.zip -d /tmp \
    && /tmp/aws/install \
    && rm -rf /tmp/aws /tmp/awscliv2.zip

# === Docker CLI (for DinD scenarios) ===
RUN curl -fsSL https://download.docker.com/linux/debian/gpg | \
    gpg --dearmor -o /etc/apt/keyrings/docker.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/debian bookworm stable" \
       > /etc/apt/sources.list.d/docker.list \
    && apt-get update && apt-get install -y --no-install-recommends docker-ce-cli \
    && rm -rf /var/lib/apt/lists/*

# === Zsh + Oh My Zsh ===
RUN apt-get update && apt-get install -y --no-install-recommends zsh \
    && rm -rf /var/lib/apt/lists/*

# === Workspace Setup ===
ARG WORKSPACE_DIR=/workspace
ENV WORKSPACE_DIR=${WORKSPACE_DIR}

# Go environment - use workspace-relative paths to avoid permission issues
ENV GOPATH="${WORKSPACE_DIR}/.go"
ENV GOMODCACHE="${WORKSPACE_DIR}/.gomodcache"
ENV PATH="${GOPATH}/bin:${PATH}"

# === Non-root User ===
# Create agent user with zsh as default shell
RUN useradd -m -s /bin/zsh agent \
    && mkdir -p ${WORKSPACE_DIR} \
    && chown agent:agent ${WORKSPACE_DIR}

USER agent

# Install Oh My Zsh for agent user (unattended)
RUN sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)" "" --unattended

# Configure zsh with useful plugins
RUN sed -i 's/plugins=(git)/plugins=(git kubectl docker golang npm python pip)/' ~/.zshrc

WORKDIR ${WORKSPACE_DIR}

# Default shell
CMD ["/bin/zsh"]
