# Claude Code Agent Image
#
# This image extends the universal base with Claude Code CLI for AI-powered tasks.
#
# Documentation:
# - Claude Code: https://code.claude.com/docs
# - Headless Mode: https://code.claude.com/docs/en/headless
#
# Environment Variables (set at runtime via KubeTask Agent secrets):
# - ANTHROPIC_API_KEY: Anthropic API key
#
ARG BASE_IMAGE=quay.io/kubetask/kubetask-agent-base:latest
FROM ${BASE_IMAGE}

# Install Claude Code CLI using the official install script
# The script installs to ~/.local/bin/claude
USER agent
RUN curl -fsSL https://claude.ai/install.sh | bash
ENV PATH="/home/agent/.local/bin:${PATH}"

# Recommended command for Agent configuration:
# command: ["sh", "-c", "claude --output-format stream-json --dangerously-skip-permissions -p \"$(cat ${WORKSPACE_DIR}/task.md)\""]
#
# Note: ENTRYPOINT is no longer used. Agent.spec.command is now required.
# The command defines HOW the agent executes tasks, allowing users to customize
# execution behavior (e.g., output format, flags) without modifying the image.
#
# Original ENTRYPOINT (kept as reference):
# ENTRYPOINT ["sh", "-c", "claude --output-format stream-json --dangerously-skip-permissions -p \"$(cat ${WORKSPACE_DIR}/task.md)\""]
