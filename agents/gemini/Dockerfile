# Gemini CLI Agent Image
#
# This image extends the universal base with Gemini CLI for AI-powered tasks.
#
# Documentation:
# - Gemini CLI: https://github.com/google-gemini/gemini-cli
#
# Environment Variables (set at runtime via KubeTask Agent secrets):
# - GOOGLE_API_KEY: Gemini API key
# - GOOGLE_CLOUD_PROJECT: GCP project for Vertex AI (optional)
#
ARG BASE_IMAGE=quay.io/kubetask/kubetask-agent-base:latest
FROM ${BASE_IMAGE}

# Install Gemini CLI globally
USER root
RUN npm install -g @google/gemini-cli

# Install stream-json-reader utility
COPY stream-json-reader.sh /usr/local/bin/stream-json-reader
RUN chmod +x /usr/local/bin/stream-json-reader

USER agent

# Pre-configure Gemini CLI settings (enable preview features and Gemini 3 Pro)
RUN mkdir -p ~/.gemini && \
    echo '{"model":{"name":"gemini-3-pro-preview"},"general":{"previewFeatures":true}}' > ~/.gemini/settings.json

# Recommended command for Agent configuration:
# command: ["sh", "-c", "gemini --output-format stream-json --yolo -p \"$(cat ${WORKSPACE_DIR}/task.md)\""]
#
# Note: ENTRYPOINT is no longer used. Agent.spec.command is now required.
# The command defines HOW the agent executes tasks, allowing users to customize
# execution behavior (e.g., output format, flags) without modifying the image.
#
# Original ENTRYPOINT (kept as reference):
# ENTRYPOINT ["sh", "-c", "gemini --output-format stream-json --yolo -p \"$(cat ${WORKSPACE_DIR}/task.md)\""]
