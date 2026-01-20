# Build the kubeopencode unified binary
FROM golang:1.25-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# Copy the go source
COPY cmd/ cmd/
COPY api/ api/
COPY internal/ internal/
COPY vendor/ vendor/

# Build using vendor directory (faster, no download needed)
# Build the unified kubeopencode binary with all subcommands
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
    -mod=vendor \
    -ldflags="-s -w" \
    -a \
    -o kubeopencode \
    ./cmd/kubeopencode/

# Runtime stage - use alpine for git and ssh (required for git-init)
FROM alpine:3.23

# Re-declare ARGs for this stage (ARGs don't persist across stages)
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

# Install git and ssh client for repository cloning (used by git-init subcommand)
RUN apk add --no-cache \
    git \
    openssh-client \
    && rm -rf /var/cache/apk/*

# Add labels for traceability
LABEL org.opencontainers.image.revision="${GIT_COMMIT}" \
      org.opencontainers.image.created="${BUILD_TIME}" \
      org.opencontainers.image.source="https://github.com/kubeopencode/kubeopencode" \
      org.opencontainers.image.title="kubeopencode" \
      org.opencontainers.image.description="KubeOpenCode - Kubernetes-native AI task execution"

# Copy the binary from builder
COPY --from=builder /workspace/kubeopencode /kubeopencode

# Create the default directories for git-init and save-session
RUN mkdir -p /git /pvc /signal && chmod 777 /git /pvc /signal

# Run as non-root user for security
RUN adduser -D -u 65532 kubeopencode
USER 65532

ENTRYPOINT ["/kubeopencode"]
