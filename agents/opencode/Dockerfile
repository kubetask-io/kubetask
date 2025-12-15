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

# Recommended command for Agent configuration:
# command: ["sh", "-c", "opencode run --format json \"$(cat ${WORKSPACE_DIR}/task.md)\""]
#
# Note: ENTRYPOINT is no longer used. Agent.spec.command is now required.
# The command defines HOW the agent executes tasks, allowing users to customize
# execution behavior (e.g., output format, provider settings) without modifying the image.
#
# Original ENTRYPOINT (kept as reference):
# ENTRYPOINT ["sh", "-c", "opencode run --format json \"$(cat ${WORKSPACE_DIR}/task.md)\""]
