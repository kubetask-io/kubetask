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
ARG BASE_IMAGE=quay.io/zhaoxue/kubetask-agent-base:latest
FROM ${BASE_IMAGE}

# Install Gemini CLI globally
USER root
RUN npm install -g @google/gemini-cli
USER agent

# Build argument for workspace directory (inherited from base)
ARG WORKSPACE_DIR=/workspace
ENV WORKSPACE_DIR=${WORKSPACE_DIR}

# Default entrypoint - run Gemini CLI in headless mode
# The agent reads the task from ${WORKSPACE_DIR}/task.md and executes it
# Using -p flag for direct prompt input from the mounted context file
# Using --yolo to auto-approve actions (safe in container environment)
# Using stream-json output to see all logs
ENTRYPOINT ["sh", "-c", "gemini --output-format stream-json --yolo -p \"$(cat ${WORKSPACE_DIR}/task.md)\""]
