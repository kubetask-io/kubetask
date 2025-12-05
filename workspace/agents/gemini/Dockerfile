# Gemini CLI Agent Image
FROM node:24-slim

# Install git, jq, gh, yq and other common tools needed for repository operations
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    jq \
    ca-certificates \
    curl \
    gpg \
    && mkdir -p /etc/apt/keyrings \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | gpg --dearmor -o /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends gh \
    && curl -fsSL https://github.com/mikefarah/yq/releases/latest/download/yq_linux_$(dpkg --print-architecture) -o /usr/local/bin/yq \
    && chmod +x /usr/local/bin/yq \
    && apt-get purge -y curl gpg \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/*

# Install Gemini CLI globally
RUN npm install -g @google/gemini-cli

# Create non-root user for security
RUN useradd -m -s /bin/bash gemini
USER gemini
WORKDIR /workspace

# Default entrypoint - run Gemini CLI in headless mode
# The agent reads the task from /workspace/task.md and executes it
# Using -p flag for direct prompt input from the mounted context file
# Using --yolo to auto-approve actions (safe in container environment)
# Using stream-json output with jq to filter assistant messages for real-time logs
ENTRYPOINT ["sh", "-c", "gemini --output-format stream-json --yolo -p \"$(cat /workspace/task.md)\" | jq -r 'select(.type == \"message\" and .role == \"assistant\") | .content'"]
