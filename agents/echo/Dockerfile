# Copyright Contributors to the KubeOpenCode project
#
# Simple echo agent for E2E testing
# This agent simply executes shell commands and echoes task content

FROM alpine:3.21

# Install minimal tools needed for testing
RUN apk add --no-cache \
    bash \
    coreutils \
    findutils \
    && rm -rf /var/cache/apk/*

# Create non-root user
RUN adduser -D -u 65532 agent

# Create workspace directory
RUN mkdir -p /workspace && chown agent:agent /workspace

USER agent
WORKDIR /workspace

# Default entrypoint - can be overridden by Agent.spec.command
ENTRYPOINT ["/bin/sh", "-c"]
CMD ["echo 'Echo agent ready'"]
