# Echo Agent Image for E2E Testing
# This is a simple test agent that echoes the content of task.md
FROM alpine:3.21

# Install minimal dependencies
RUN apk add --no-cache bash

# Build argument for workspace directory
ARG WORKSPACE_DIR=/workspace

# Set workspace directory as environment variable for runtime use
ENV WORKSPACE_DIR=${WORKSPACE_DIR}

# Create non-root user for security
RUN adduser -D -s /bin/bash agent
USER agent
WORKDIR ${WORKSPACE_DIR}

# Default entrypoint - echo the content of all files in the workspace directory
# This is useful for e2e testing to verify files are correctly mounted
# It recursively finds all files and prints their path and content
ENTRYPOINT ["sh", "-c", "echo '=== Task Content ===' && find ${WORKSPACE_DIR} -type f -print0 2>/dev/null | sort -z | xargs -0 -I {} sh -c 'echo \"--- File: {} ---\" && cat \"{}\" && echo' && echo '=== Task Completed ==='"]
