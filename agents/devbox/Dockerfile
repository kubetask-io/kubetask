# KubeOpenCode Universal Agent Base Image
#
# This base image provides a comprehensive development environment similar to
# GitHub Actions runners and devcontainer images. It includes:
# - Multi-language runtimes: Go, Node.js, Python
# - Cloud CLIs: gcloud, aws, kubectl, helm
# - Development tools: git, gh, make, gcc, jq, yq
# - Shell: zsh
# - OpenShift compatibility: Works with arbitrary UIDs
#
# AI agent images (opencode, etc.) build on top of this base.
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

# === Zsh ===
RUN apt-get update && apt-get install -y --no-install-recommends zsh \
    && rm -rf /var/lib/apt/lists/*

# === GitHub App Authentication Scripts ===
# These scripts provide transparent GitHub authentication for gh CLI and git.
# When GitHub App credentials are mounted to /etc/github-app/, the scripts
# automatically generate and cache Installation Access Tokens.
#
# Scripts:
#   - github-app-iat.sh: Generate IAT from GitHub App credentials
#   - github-token-manager.sh: Cache and refresh tokens automatically
#   - gh-wrapper.sh: Inject GH_TOKEN transparently for gh CLI
#   - git-credential-github-app.sh: Git credential helper for transparent auth
#
# Usage: Just mount credentials to /etc/github-app/ and use gh/git normally
COPY --chmod=755 scripts/*.sh /usr/local/bin/

# === Slack CLI ===
# Zero-dependency Python script for Slack interaction
COPY --chmod=755 scripts/slack-cli.py /usr/local/bin/slack-cli

# Setup gh wrapper: gh command transparently gets token from GitHub App
# Move real gh to gh-real, then link wrapper as gh
RUN mv /usr/bin/gh /usr/bin/gh-real \
    && ln -sf /usr/local/bin/gh-wrapper.sh /usr/bin/gh

# Setup git credential helper: git push/pull transparently authenticated
RUN git config --system credential.helper /usr/local/bin/git-credential-github-app.sh

# === Workspace Setup ===
ARG WORKSPACE_DIR=/workspace
ENV WORKSPACE_DIR=${WORKSPACE_DIR}

# Go environment - use /tmp for OpenShift compatibility
#
# Why /tmp instead of /workspace?
# 1. OpenShift runs containers with arbitrary UIDs (e.g., 1000570001)
#    These random UIDs may not have write permissions to /workspace subdirectories
# 2. /tmp is world-writable (chmod 777) by default, so any UID can write to it
# 3. Go downloads dependencies as read-only files to prevent accidental modification
#    When `make update` runs `gofmt -w` on all Go files (including .gomodcache),
#    it fails because dependencies are read-only. Using /tmp isolates these caches.
# 4. Argo Workflows clone git repos to /workspace, which may overwrite or conflict
#    with .go and .gomodcache directories
# 5. This is consistent with HOME="/tmp" set below for the same reason
#
ENV GOPATH="/tmp/.go"
ENV GOMODCACHE="/tmp/.gomodcache"
ENV GOCACHE="/tmp/.cache/go-build"
ENV PATH="/tmp/.go/bin:${PATH}"

# === Non-root User Setup (OpenShift Compatible) ===
# OpenShift runs containers with arbitrary UIDs from a project-specific range.
# Solution: Use /tmp as HOME since it's world-writable by default.
# This avoids all permission issues with arbitrary UIDs.

# Create workspace directory (will be writable via securityContext in K8s)
RUN mkdir -p ${WORKSPACE_DIR} && chmod 777 ${WORKSPACE_DIR}

# === Environment Variables ===
# Use /tmp as HOME to avoid permission issues with OpenShift arbitrary UIDs
# /tmp is world-writable, so any UID can create config files there
ENV HOME="/tmp"
ENV SHELL="/bin/zsh"
ENV XDG_CONFIG_HOME="/tmp/.config"
ENV XDG_CACHE_HOME="/tmp/.cache"
ENV XDG_DATA_HOME="/tmp/.local/share"

# Pre-create config and cache directories in /tmp with proper permissions
RUN mkdir -p /tmp/.config /tmp/.cache /tmp/.local/share \
    /tmp/.go /tmp/.gomodcache /tmp/.cache/go-build \
    && chmod -R 777 /tmp/.config /tmp/.cache /tmp/.local /tmp/.go /tmp/.gomodcache

# Switch to non-root user (any UID works with /tmp as HOME)
USER 1000:0

WORKDIR ${WORKSPACE_DIR}

# Default shell
CMD ["/bin/zsh"]
