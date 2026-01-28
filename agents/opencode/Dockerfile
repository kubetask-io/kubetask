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
#   - Linux x64: opencode-linux-x64.tar.gz (glibc)
#   - Linux arm64: opencode-linux-arm64.tar.gz (glibc)
#
# Note: Using glibc variants for Debian-based executor (devbox) compatibility.
# The init container uses Debian to ensure binary compatibility with glibc-based work containers.
#
# Version Management:
#   Set OPENCODE_VERSION build arg to pin to a specific version.
#   Example: docker build --build-arg OPENCODE_VERSION=1.1.23 -t opencode .
#

FROM debian:bookworm-slim

# OpenCode version to download (use specific version to avoid cache issues)
# Override with: --build-arg OPENCODE_VERSION=x.y.z
ARG OPENCODE_VERSION=1.1.39

# Install curl for downloading
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Detect architecture and download appropriate binary
ARG TARGETARCH
RUN set -ex; \
    case "${TARGETARCH}" in \
        amd64) ARCH="x64" ;; \
        arm64) ARCH="arm64" ;; \
        *) echo "Unsupported architecture: ${TARGETARCH}" && exit 1 ;; \
    esac; \
    FILENAME="opencode-linux-${ARCH}.tar.gz"; \
    URL="https://github.com/anomalyco/opencode/releases/download/v${OPENCODE_VERSION}/${FILENAME}"; \
    echo "Downloading OpenCode v${OPENCODE_VERSION} from ${URL}..."; \
    curl -fsSL "${URL}" -o /tmp/opencode.tar.gz; \
    tar -xzf /tmp/opencode.tar.gz -C /tmp; \
    mv /tmp/opencode /opencode; \
    chmod +x /opencode; \
    rm -f /tmp/opencode.tar.gz; \
    # Verify the binary
    /opencode --version

# Remove curl to minimize image size
RUN apt-get purge -y curl && apt-get autoremove -y && rm -rf /var/lib/apt/lists/*

# Default environment variables
ENV TOOLS_DIR=/tools
ENV OPENCODE_BIN=opencode

# Copy entrypoint script
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
