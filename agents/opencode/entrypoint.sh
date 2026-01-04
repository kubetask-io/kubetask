#!/bin/sh
# OpenCode Init Container Entrypoint
#
# Copies the opencode binary to the shared tools volume at /tools/opencode.
# The work container can then execute: /tools/opencode run ...
#
# Environment Variables:
#   TOOLS_DIR     - Target directory (default: /tools)
#   OPENCODE_BIN  - Binary name (default: opencode)

set -e

TOOLS_DIR="${TOOLS_DIR:-/tools}"
OPENCODE_BIN="${OPENCODE_BIN:-opencode}"
TARGET="${TOOLS_DIR}/${OPENCODE_BIN}"

echo "[opencode-init] Copying OpenCode binary to ${TARGET}..."
mkdir -p "${TOOLS_DIR}"
cp /opencode "${TARGET}"
chmod +x "${TARGET}"

echo "[opencode-init] OpenCode binary installed successfully."
echo "[opencode-init] Work containers can use: ${TARGET}"

# Print version for verification
"${TARGET}" --version 2>/dev/null || echo "[opencode-init] Version check skipped"
