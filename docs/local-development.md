# Local Development Environment Setup

This guide describes how to set up a local development environment for KubeTask using Kind (Kubernetes in Docker).

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
kind create cluster --name kubetask
```

Verify the cluster is running:

```bash
kubectl cluster-info
```

**Note:** The examples below use `--name kubetask` for Kind commands. If using an existing cluster with a different name (e.g., `kind`), replace `--name kubetask` with your cluster name.

### 2. Build Images

Build all required images:

```bash
# Build the controller image
make docker-build

# Build the kubetask-tools image (provides git-init, save-session, etc.)
make tools-build

# Build the agent image (gemini is the default)
make agent-build
```

**Note:** The kubetask-tools image provides multiple utilities:
- `git-init`: Git repository cloning for Git Context
- `save-session`: Workspace persistence for session resume

If you only use Inline or ConfigMap contexts and don't need session persistence, you can skip building it.

### 3. Load Images to Kind

Load images into the Kind cluster (required because Kind cannot pull from local Docker):

```bash
kind load docker-image quay.io/kubetask/kubetask-controller:latest --name kubetask
kind load docker-image quay.io/kubetask/kubetask-tools:latest --name kubetask
kind load docker-image quay.io/kubetask/kubetask-agent-gemini:latest --name kubetask
```

### 4. Deploy with Helm

```bash
helm upgrade --install kubetask ./charts/kubetask \
  --namespace kubetask-system \
  --create-namespace \
  --set controller.image.pullPolicy=Never \
  --set agent.image.repository=quay.io/kubetask/kubetask-agent-gemini \
  --set agent.image.pullPolicy=Never
```

### 5. Verify Deployment

Check the controller is running:

```bash
kubectl get pods -n kubetask-system
```

Expected output:

```
NAME                                   READY   STATUS    RESTARTS   AGE
kubetask-controller-xxxxxxxxx-xxxxx   1/1     Running   0          30s
```

Check CRDs are installed:

```bash
kubectl get crds | grep kubetask
```

Expected output:

```
agents.kubetask.io            <timestamp>
contexts.kubetask.io          <timestamp>
crontasks.kubetask.io         <timestamp>
kubetaskconfigs.kubetask.io   <timestamp>
tasks.kubetask.io             <timestamp>
```

Check controller logs:

```bash
kubectl logs -n kubetask-system deployment/kubetask-controller
```

## Iterative Development

When you make changes to the controller code:

```bash
# Rebuild the image
make docker-build

# Reload into Kind
kind load docker-image quay.io/kubetask/kubetask-controller:latest --name kubetask

# Restart the deployment to pick up the new image
kubectl rollout restart deployment/kubetask-controller -n kubetask-system

# Watch the rollout
kubectl rollout status deployment/kubetask-controller -n kubetask-system
```

Or use the convenience target:

```bash
make e2e-reload
```

## Testing a Task

Create a test namespace and service account:

```bash
kubectl create namespace test
kubectl create serviceaccount task-runner -n test
```

Create an Agent:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: gemini-agent
  namespace: test
spec:
  agentImage: quay.io/kubetask/kubetask-agent-gemini:latest
  serviceAccountName: task-runner
EOF
```

Create a Task:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: hello-world
  namespace: test
spec:
  agentRef:
    name: gemini-agent
  prompt: "Hello, KubeTask!"
EOF
```

Check Task status:

```bash
kubectl get task -n test hello-world -o yaml
```

Check Job logs:

```bash
kubectl logs -n test -l kubetask.io/task=hello-world
```

## Cleanup

Uninstall KubeTask:

```bash
helm uninstall kubetask -n kubetask-system
kubectl delete namespace kubetask-system
```

Delete the Kind cluster:

```bash
kind delete cluster --name kubetask
```

## Troubleshooting

### Image Pull Errors

If you see `ErrImagePull` or `ImagePullBackOff`, ensure:

1. Images are loaded into Kind: `docker exec kind-control-plane crictl images | grep kubetask`
2. `imagePullPolicy` is set to `Never` in Helm values

### Controller Not Starting

Check controller logs:

```bash
kubectl logs -n kubetask-system deployment/kubetask-controller
```

Check events:

```bash
kubectl get events -n kubetask-system --sort-by='.lastTimestamp'
```

### CRDs Not Found

Ensure CRDs are installed:

```bash
kubectl get crds | grep kubetask
```

If missing, reinstall with Helm or apply manually:

```bash
kubectl apply -f deploy/crds/
```
