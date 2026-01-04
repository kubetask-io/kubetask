# OpenCode Tool Init Container
#
# This minimal image provides the OpenCode CLI binary for copying to shared volumes.
# Used as an init container to provision OpenCode to work containers.
#
# Architecture:
#   Init Container (this image) → copies → /tools/opencode → Work Container uses it
#
# Usage:
#   # As init container in Kubernetes Pod:
#   initContainers:
#     - name: opencode-init
#       image: quay.io/kubeopencode/kubeopencode-agent-opencode:latest
#       volumeMounts:
#         - name: tools
#           mountPath: /tools
#   containers:
#     - name: agent
#       image: quay.io/kubeopencode/kubeopencode-agent-common-dev:latest
#       command: ["/tools/opencode", "run", "--format", "json", "..."]
#       volumeMounts:
#         - name: tools
#           mountPath: /tools
#
# Binary Source:
#   Downloaded from GitHub Releases: https://github.com/anomalyco/opencode/releases
#   - Linux x64: opencode-linux-x64-musl.tar.gz
#   - Linux arm64: opencode-linux-arm64-musl.tar.gz
#
# Note: Using musl variants for Alpine Linux compatibility.
#

FROM alpine:3.21

# Install runtime dependencies required by OpenCode binary
# libstdc++ and libgcc are needed as the binary is dynamically linked
RUN apk add --no-cache libstdc++ libgcc

# Install curl for downloading (temporary)
RUN apk add --no-cache curl tar

# Detect architecture and download appropriate binary
ARG TARGETARCH
RUN set -ex; \
    case "${TARGETARCH}" in \
        amd64) ARCH="x64" ;; \
        arm64) ARCH="arm64" ;; \
        *) echo "Unsupported architecture: ${TARGETARCH}" && exit 1 ;; \
    esac; \
    FILENAME="opencode-linux-${ARCH}-musl.tar.gz"; \
    URL="https://github.com/anomalyco/opencode/releases/latest/download/${FILENAME}"; \
    echo "Downloading OpenCode from ${URL}..."; \
    curl -fsSL "${URL}" -o /tmp/opencode.tar.gz; \
    tar -xzf /tmp/opencode.tar.gz -C /tmp; \
    mv /tmp/opencode /opencode; \
    chmod +x /opencode; \
    rm -f /tmp/opencode.tar.gz; \
    # Verify the binary
    /opencode --version

# Remove curl to minimize image size (keep libstdc++ and libgcc)
RUN apk del curl tar

# Default environment variables
ENV TOOLS_DIR=/tools
ENV OPENCODE_BIN=opencode

# Copy entrypoint script
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
