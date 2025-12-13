# Goose Agent Image
#
# This image extends the universal base with Goose CLI for AI-powered tasks.
# Goose is an open-source AI agent by Block that runs locally.
#
# Documentation:
# - Goose Homepage: https://block.github.io/goose/
# - Running Tasks: https://block.github.io/goose/docs/guides/running-tasks
# - Headless Mode: https://block.github.io/goose/docs/tutorials/headless-goose/
# - Providers: https://block.github.io/goose/docs/getting-started/providers
# - GitHub: https://github.com/block/goose
#
# Environment Variables (set at runtime via KubeTask Agent secrets):
# - GOOSE_PROVIDER: AI provider (anthropic, openai, google, etc.)
# - GOOSE_MODEL: Model name (e.g., claude-sonnet-4-20250514)
# - API keys: ANTHROPIC_API_KEY, OPENAI_API_KEY, etc.
#
ARG BASE_IMAGE=quay.io/kubetask/kubetask-agent-base:latest
FROM ${BASE_IMAGE}

# Install Goose CLI
# Goose requires libxcb and related X11 libraries even in headless mode
USER root
RUN apt-get update && apt-get install -y --no-install-recommends \
    libxcb1 \
    libxcb-render0 \
    libxcb-shape0 \
    libxcb-xfixes0 \
    && rm -rf /var/lib/apt/lists/* \
    && curl -fsSL https://github.com/block/goose/releases/download/stable/download_cli.sh | CONFIGURE=false bash \
    && mv /root/.local/bin/goose /usr/local/bin/goose
USER agent

# Build argument for workspace directory (inherited from base)
ARG WORKSPACE_DIR=/workspace
ENV WORKSPACE_DIR=${WORKSPACE_DIR}

# Goose environment defaults
# GOOSE_MODE=auto: auto-approve operations (safe in container environment)
# GOOSE_CONTEXT_STRATEGY=summarize: manage context efficiently
# Provider and model are set at runtime via secrets (GOOSE_PROVIDER, GOOSE_MODEL, API keys)
ENV GOOSE_MODE=auto
ENV GOOSE_CONTEXT_STRATEGY=summarize

# Default entrypoint - run Goose in headless mode
# The agent reads the task from ${WORKSPACE_DIR}/task.md and executes it
# Using --no-session for one-off tasks (no session history clutter)
# Using -i flag to read instructions from file
ENTRYPOINT ["sh", "-c", "goose run --no-session -i ${WORKSPACE_DIR}/task.md"]
