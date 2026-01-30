# Local Development Guide

This guide describes how to set up a local development environment for KubeOpenCode using Kind (Kubernetes in Docker).

## Prerequisites

- Docker
- Kind (`brew install kind` on macOS)
- kubectl
- Helm 3.x
- Go 1.25+

## Quick Start

### 1. Create or Use Existing Kind Cluster

Check if you already have a Kind cluster running:

```bash
kind get clusters
```

If you have an existing cluster (e.g., `kind`), you can use it directly. Otherwise, create a new one:

```bash
kind create cluster --name kubeopencode
```

Verify the cluster is running:

```bash
kubectl cluster-info
```

**Note:** The examples below use `--name kubeopencode` for Kind commands. If using an existing cluster with a different name (e.g., `kind`), replace `--name kubeopencode` with your cluster name.

### 2. Build Images

Build all required images:

```bash
# Build the controller image
make docker-build

# Build the agent images (two-container pattern)
make agent-build AGENT=opencode    # OpenCode init container
make agent-build AGENT=devbox      # Executor container
make agent-build AGENT=attach      # Lightweight attach image (Server mode)
```

**Note:** The unified kubeopencode image provides both controller and infrastructure utilities:
- `kubeopencode controller`: Kubernetes controller
- `kubeopencode git-init`: Git repository cloning for Git Context
- `kubeopencode save-session`: Workspace persistence for session resume

### 3. Load Images to Kind

Load images into the Kind cluster (required because Kind cannot pull from local Docker):

```bash
kind load docker-image quay.io/kubeopencode/kubeopencode:latest --name kubeopencode
kind load docker-image quay.io/kubeopencode/kubeopencode-agent-opencode:latest --name kubeopencode
kind load docker-image quay.io/kubeopencode/kubeopencode-agent-devbox:latest --name kubeopencode
kind load docker-image quay.io/kubeopencode/kubeopencode-agent-attach:latest --name kubeopencode
```

### 4. Deploy with Helm

```bash
helm upgrade --install kubeopencode ./charts/kubeopencode \
  --namespace kubeopencode-system \
  --create-namespace \
  --set controller.image.pullPolicy=Never \
  --set agent.image.pullPolicy=Never \
  --set server.enabled=true
```

> **Note:** The `server.enabled=true` deploys the UI server. You can omit it if you only need the controller.

### 5. Verify Deployment

Check the controller is running:

```bash
kubectl get pods -n kubeopencode-system
```

Expected output:

```
NAME                                       READY   STATUS    RESTARTS   AGE
kubeopencode-controller-xxxxxxxxx-xxxxx    1/1     Running   0          30s
kubeopencode-server-xxxxxxxxx-xxxxx        1/1     Running   0          30s
```

> **Note:** If `server.enabled=false`, only the controller pod will be present.

Check CRDs are installed:

```bash
kubectl get crds | grep kubeopencode
```

Expected output:

```
agents.kubeopencode.io            <timestamp>
kubeopencodeconfigs.kubeopencode.io   <timestamp>
tasks.kubeopencode.io             <timestamp>
```

Check controller logs:

```bash
kubectl logs -n kubeopencode-system deployment/kubeopencode-controller
```

## UI Server

KubeOpenCode includes a web UI for managing Tasks and viewing Agents. The UI is an "Agent as Application" platform that allows non-technical users to interact with AI agents.

### Access the UI

#### Option 1: Port Forward (Quick Access)

```bash
kubectl port-forward -n kubeopencode-system svc/kubeopencode-server 2746:2746
```

Then open http://localhost:2746 in your browser.

#### Option 2: NodePort (Kind Cluster)

Update Helm values to expose via NodePort:

```bash
helm upgrade kubeopencode ./charts/kubeopencode \
  --namespace kubeopencode-system \
  --set server.enabled=true \
  --set server.service.type=NodePort \
  --set server.service.nodePort=32746
```

Access via: http://localhost:32746

#### Option 3: Ingress

Enable ingress in Helm values:

```bash
helm upgrade kubeopencode ./charts/kubeopencode \
  --namespace kubeopencode-system \
  --set server.enabled=true \
  --set server.ingress.enabled=true \
  --set server.ingress.hosts[0].host=kubeopencode.local \
  --set server.ingress.hosts[0].paths[0].path=/ \
  --set server.ingress.hosts[0].paths[0].pathType=Prefix
```

Add to `/etc/hosts`:
```
127.0.0.1 kubeopencode.local
```

### UI Features

| Feature | Description |
|---------|-------------|
| **Task List** | View all Tasks across namespaces with status filtering |
| **Task Detail** | View Task details, logs (real-time streaming) |
| **Task Create** | Create new Tasks with Agent selection (filtered by namespace permissions) |
| **Agent List** | Browse available Agents with namespace filter |
| **Agent Detail** | View Agent configuration, contexts, credentials |

#### Namespace Filtering

The Agent list page includes a namespace selector that allows you to filter Agents by namespace. This helps in multi-tenant environments where different teams have Agents in different namespaces.

#### Agent Availability

When creating a Task, only Agents that are available for the selected Task namespace are shown. Availability is determined by the Agent's `allowedNamespaces` field:

- If an Agent has no `allowedNamespaces` defined, it is available to all namespaces
- If an Agent has `allowedNamespaces` defined, it is only available to namespaces that match the patterns (glob patterns supported)

### Authentication

The UI uses ServiceAccount token authentication. For local development with port-forward, the server operates in a permissive mode suitable for testing.

For production, enable authentication with RBAC-based filtering:

```bash
helm upgrade kubeopencode ./charts/kubeopencode \
  --namespace kubeopencode-system \
  --set server.enabled=true \
  --set server.authEnabled=true \
  --set server.authAllowAnonymous=false
```

When authentication is enabled:
- Users must provide a Bearer token in the Authorization header
- The token is validated using the Kubernetes TokenReview API
- API requests are executed with user impersonation, respecting Kubernetes RBAC
- Users only see Agents and Tasks they have permission to access

### UI Development

To develop the UI locally with hot-reload:

```bash
# Terminal 1: Run the Go server (API backend)
make run-server

# Terminal 2: Run webpack dev server (frontend with hot-reload)
make ui-dev
```

The webpack dev server runs on http://localhost:3000 and proxies API requests to the Go server on port 2746.

To build the UI for production:

```bash
make ui-build
```

## Iterative Development

### Controller Changes

When you make changes to the controller code:

```bash
# Rebuild the image
make docker-build

# Reload into Kind
kind load docker-image quay.io/kubeopencode/kubeopencode:latest --name kubeopencode

# Restart the deployment to pick up the new image
kubectl rollout restart deployment/kubeopencode-controller -n kubeopencode-system

# Watch the rollout
kubectl rollout status deployment/kubeopencode-controller -n kubeopencode-system
```

Or use the convenience target:

```bash
make e2e-reload
```

### UI Changes

When you make changes to the UI code:

```bash
# Rebuild the UI and docker image
make ui-build
make docker-build

# Reload into Kind
kind load docker-image quay.io/kubeopencode/kubeopencode:latest --name kubeopencode

# Restart the server deployment
kubectl rollout restart deployment/kubeopencode-server -n kubeopencode-system
```

For faster iteration during UI development, use the dev server instead:

```bash
# Run Go server locally (uses kubeconfig for API access)
make run-server

# In another terminal, run webpack dev server with hot-reload
make ui-dev
```

This provides instant feedback without rebuilding Docker images.

## Local Test Environment

For quick testing, use the pre-configured resources in `deploy/local-dev/`:

### Deploy Test Resources

```bash
# First, create secrets.yaml from template
cp deploy/local-dev/secrets.yaml.example deploy/local-dev/secrets.yaml
# Edit secrets.yaml with your real API keys
vim deploy/local-dev/secrets.yaml

# Deploy all resources (namespace, secrets, RBAC, agents)
kubectl apply -k deploy/local-dev/

# Verify the Agent is ready (for Server mode)
kubectl get agent -n test
kubectl get deployment -n test
```

### Resources Created

| Resource | Name | Description |
|----------|------|-------------|
| Namespace | `test` | Isolated namespace for testing |
| Secret | `opencode-credentials` | OpenCode API key |
| Secret | `git-settings` | Git author/committer settings |
| ServiceAccount | `kubeopencode-agent` | Agent service account |
| Role/RoleBinding | `kubeopencode-agent` | RBAC permissions |
| Agent | `server-agent` | Server-mode agent (persistent) |
| Agent | `pod-agent` | Pod-mode agent (per-task) |

### Test Tasks

#### Server Mode Test

```bash
kubectl apply -n test -f - <<EOF
apiVersion: kubeopencode.io/v1alpha1
kind: Task
metadata:
  name: server-test
spec:
  agentRef:
    name: server-agent
  description: "Say hello world"
EOF

# Check status
kubectl get task -n test
kubectl logs -n test server-test-pod -c agent
```

#### Pod Mode Test

```bash
kubectl apply -n test -f - <<EOF
apiVersion: kubeopencode.io/v1alpha1
kind: Task
metadata:
  name: pod-test
spec:
  agentRef:
    name: pod-agent
  description: "What is 2+2?"
EOF

# Check status
kubectl get task -n test
kubectl logs -n test pod-test-pod -c agent
```

#### Concurrent Tasks Test

```bash
for i in 1 2 3; do
  kubectl apply -n test -f - <<EOF
apiVersion: kubeopencode.io/v1alpha1
kind: Task
metadata:
  name: concurrent-$i
spec:
  agentRef:
    name: server-agent
  description: "Count to $i"
EOF
done

# Watch progress
kubectl get task -n test -w
```

### Customization

#### Using Real Secrets

Create a local secrets file (gitignored):

```bash
cp deploy/local-dev/secrets.yaml deploy/local-dev/secrets.local.yaml
# Edit secrets.local.yaml with real values
kubectl apply -f deploy/local-dev/secrets.local.yaml -n test
```

#### Different AI Model

Edit `agent-server.yaml` or `agent-pod.yaml` to change the model:

```yaml
config: |
  {
    "$schema": "https://opencode.ai/config.json",
    "model": "anthropic/claude-sonnet-4-20250514",
    "small_model": "anthropic/claude-haiku-4-20250514"
  }
```

## Cleanup

### Delete Test Resources

```bash
# Delete all tasks
kubectl delete task --all -n test

# Delete all test resources
kubectl delete -k deploy/local-dev/
```

### Uninstall KubeOpenCode

```bash
helm uninstall kubeopencode -n kubeopencode-system
kubectl delete namespace kubeopencode-system
```

### Delete Kind Cluster

```bash
kind delete cluster --name kubeopencode
```

## Debugging Tools

### Reading OpenCode Stream JSON Output

When running Tasks with `--format json`, the output is in stream-json format which can be hard to read. We provide a utility script to format the output:

```bash
# Read from kubectl logs
kubectl logs <pod-name> -n kubeopencode-system | ./hack/opencode-stream-reader.sh

# Read from a saved log file
cat task-output.log | ./hack/opencode-stream-reader.sh
```

The script requires `jq` and converts the JSON stream into human-readable output with colors and formatting.

## Troubleshooting

### Image Pull Errors

If you see `ErrImagePull` or `ImagePullBackOff`, ensure:

1. Images are loaded into Kind: `docker exec kind-control-plane crictl images | grep kubeopencode`
2. `imagePullPolicy` is set to `Never` in Helm values

### Controller Not Starting

Check controller logs:

```bash
kubectl logs -n kubeopencode-system deployment/kubeopencode-controller
```

Check events:

```bash
kubectl get events -n kubeopencode-system --sort-by='.lastTimestamp'
```

### CRDs Not Found

Ensure CRDs are installed:

```bash
kubectl get crds | grep kubeopencode
```

If missing, reinstall with Helm or apply manually:

```bash
kubectl apply -f deploy/crds/
```
