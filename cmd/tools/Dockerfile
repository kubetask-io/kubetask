# Copyright Contributors to the KubeTask project
# kubetask-tools - Unified infrastructure tools for KubeTask
#
# This image provides multiple utility commands:
#   - git-init: Clone Git repositories for Git Context
#   - save-session: Save workspace to PVC for session persistence
#
# Size: ~20-25MB (alpine + git + openssh + go binary)

# Build stage
FROM golang:1.25-alpine AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Copy go mod files
COPY go.mod go.mod
COPY go.sum go.sum

# Download dependencies
RUN go mod download

# Copy the tools source
COPY cmd/tools/ cmd/tools/

# Build the binary
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -ldflags="-s -w" -o kubetask-tools ./cmd/tools/

# Runtime stage - use alpine for git and ssh
FROM alpine:3.21

# Install git and ssh client for repository cloning
RUN apk add --no-cache \
    git \
    openssh-client \
    && rm -rf /var/cache/apk/*

# Add labels for traceability
LABEL org.opencontainers.image.source="https://github.com/kubetask-io/kubetask" \
      org.opencontainers.image.title="kubetask-tools" \
      org.opencontainers.image.description="KubeTask Tools - Unified infrastructure tools for KubeTask"

# Copy the binary from builder
COPY --from=builder /workspace/kubetask-tools /kubetask-tools

# Create the default directories
RUN mkdir -p /git /pvc /signal && chmod 777 /git /pvc /signal

# Run as non-root user for security
RUN adduser -D -u 65532 kubetask
USER 65532

ENTRYPOINT ["/kubetask-tools"]
