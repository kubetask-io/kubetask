# code-server Sidecar Image
#
# This image provides a browser-based VSCode experience for humanInTheLoop debugging.
# It extends the dev image with code-server for remote development.
#
# Documentation:
# - code-server: https://github.com/coder/code-server
#
# Usage in Agent configuration:
#
#   humanInTheLoop:
#     enabled: true
#     image: quay.io/kubeopencode/kubeopencode-agent-code-server:latest
#     command:
#       - sh
#       - -c
#       - code-server --bind-addr 0.0.0.0:8080 ${WORKSPACE_DIR} & sleep 7200
#     ports:
#       - name: code-server
#         containerPort: 8080
#
# Access via:
#   kubectl port-forward pod/<pod-name> 8080:8080
#   Then open: http://localhost:8080
#
ARG BASE_IMAGE=quay.io/kubeopencode/kubeopencode-agent-devbox:latest
FROM ${BASE_IMAGE}

USER root

# Install code-server
RUN curl -fsSL https://code-server.dev/install.sh | sh

USER 1000:0

# Configure code-server: disable auth, bind to all interfaces
RUN mkdir -p /tmp/.config/code-server && \
    echo 'bind-addr: 0.0.0.0:8080' > /tmp/.config/code-server/config.yaml && \
    echo 'auth: none' >> /tmp/.config/code-server/config.yaml && \
    echo 'cert: false' >> /tmp/.config/code-server/config.yaml

# No ENTRYPOINT - command is specified by user in Agent.humanInTheLoop.command
