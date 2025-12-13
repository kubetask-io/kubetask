# OpenCode Agent Image
#
# This image extends the universal base with OpenCode CLI for AI-powered tasks.
# OpenCode is an open-source AI coding agent supporting 75+ LLM providers.
#
# Documentation:
# - OpenCode: https://opencode.ai/docs
# - CLI Reference: https://opencode.ai/docs/cli/
# - Providers: https://opencode.ai/docs/providers
#
# Environment Variables (set at runtime via KubeTask Agent secrets):
# - ANTHROPIC_API_KEY: Anthropic API key (for Claude models)
# - OPENAI_API_KEY: OpenAI API key
# - GOOGLE_API_KEY: Google API key (for Gemini models)
# - Other provider API keys as needed
#
ARG BASE_IMAGE=quay.io/kubetask/kubetask-agent-base:latest
FROM ${BASE_IMAGE}

# Install OpenCode CLI using npm (globally)
USER root
RUN npm install -g opencode-ai@latest
USER agent

# Build argument for workspace directory (inherited from base)
ARG WORKSPACE_DIR=/workspace
ENV WORKSPACE_DIR=${WORKSPACE_DIR}

# Default entrypoint - run OpenCode in non-interactive mode
# The agent reads the task from ${WORKSPACE_DIR}/task.md and executes it
# Using 'run' command for non-interactive mode
# Using --format json for structured output
# Note: In non-interactive mode, all permissions are auto-approved
ENTRYPOINT ["sh", "-c", "opencode run --format json \"$(cat ${WORKSPACE_DIR}/task.md)\""]
