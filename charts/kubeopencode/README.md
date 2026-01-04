# KubeOpenCode Helm Chart

This Helm chart deploys KubeOpenCode, a Kubernetes-native system for executing AI-powered tasks.

## Prerequisites

- Kubernetes 1.25+
- Helm 3.8+
- GitHub Personal Access Token (optional, for repository operations)
- Anthropic Claude API key or Vertex AI credentials

## Installing the Chart

### Quick Start

```bash
# Create namespace
kubectl create namespace kubeopencode-system

# Install from OCI registry
helm install kubeopencode oci://quay.io/kubeopencode/helm-charts/kubeopencode \
  --namespace kubeopencode-system

# Or install from local chart (for development)
helm install kubeopencode ./charts/kubeopencode \
  --namespace kubeopencode-system
```

### Production Installation

```bash
# Create a values file with your configuration
cat > my-values.yaml <<EOF
controller:
  image:
    repository: quay.io/kubeopencode/kubeopencode
    tag: latest

  resources:
    limits:
      cpu: 1000m
      memory: 1Gi
    requests:
      cpu: 200m
      memory: 256Mi

cleanup:
  enabled: true
  schedule: "0 2 * * *"  # Daily at 2 AM
  ttlDays: 7
EOF

# Install the chart
helm install kubeopencode ./charts/kubeopencode \
  --namespace kubeopencode-system \
  --values my-values.yaml
```

## Configuration

The following table lists the configurable parameters of the KubeOpenCode chart and their default values.

### Controller Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.image.repository` | Controller image repository | `quay.io/kubeopencode/kubeopencode` |
| `controller.image.tag` | Controller image tag | `""` (uses chart appVersion) |
| `controller.image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `controller.replicas` | Number of controller replicas | `1` |
| `controller.resources.limits.cpu` | CPU limit | `500m` |
| `controller.resources.limits.memory` | Memory limit | `512Mi` |
| `controller.resources.requests.cpu` | CPU request | `100m` |
| `controller.resources.requests.memory` | Memory request | `128Mi` |

### Agent Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `agent.image.repository` | Agent image repository | `quay.io/kubeopencode/kubeopencode-agent-gemini` |
| `agent.image.tag` | Agent image tag | `latest` |

### Cleanup Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `cleanup.enabled` | Enable automatic cleanup CronJob | `true` |
| `cleanup.schedule` | Cron schedule for cleanup | `"0 2 * * *"` |
| `cleanup.ttlDays` | TTL for completed Tasks (days) | `3` |
| `cleanup.failedTTLDays` | TTL for failed Tasks (days) | `7` |

## Usage Examples

### Creating an Agent

```yaml
apiVersion: kubeopencode.io/v1alpha1
kind: Agent
metadata:
  name: default
  namespace: kubeopencode-system
spec:
  agentImage: quay.io/myorg/claude-agent:v1.0
  serviceAccountName: kubeopencode-agent
  credentials:
    - name: anthropic-api-key
      secretRef:
        name: ai-credentials
        key: anthropic-key
      env: ANTHROPIC_API_KEY
    - name: github-token
      secretRef:
        name: github-creds
        key: token
      env: GITHUB_TOKEN
```

### Creating a Task

```yaml
apiVersion: kubeopencode.io/v1alpha1
kind: Task
metadata:
  name: update-deps
  namespace: kubeopencode-system
spec:
  contexts:
    # Task description
    - type: File
      file:
        filePath: /workspace/task.md
        source:
          inline: |
            Update go.mod to Go 1.21 and run go mod tidy.
            Ensure all tests pass after the upgrade.

    # Workflow guide from ConfigMap
    - type: File
      file:
        filePath: /workspace/guide.md
        source:
          configMapKeyRef:
            name: workflow-guides
            key: pr-workflow.md

    # Multiple config files as directory
    - type: File
      file:
        dirPath: /workspace/configs
        source:
          configMapRef:
            name: project-configs
```

### Batch Operations with Helm

For running the same task across multiple targets, use Helm templating:

```yaml
# values.yaml
tasks:
  - name: update-service-a
    repo: service-a
  - name: update-service-b
    repo: service-b
  - name: update-service-c
    repo: service-c

# templates/tasks.yaml
{{- range .Values.tasks }}
---
apiVersion: kubeopencode.io/v1alpha1
kind: Task
metadata:
  name: {{ .name }}
spec:
  contexts:
    - type: File
      file:
        filePath: /workspace/task.md
        source:
          inline: "Update dependencies for {{ .repo }}"
{{- end }}
```

```bash
# Generate and apply multiple tasks
helm template my-tasks ./chart | kubectl apply -f -
```

### Monitoring Progress

```bash
# Watch Task status
kubectl get tasks -n kubeopencode-system -w

# View detailed status
kubectl describe task update-deps -n kubeopencode-system

# Check Jobs
kubectl get jobs -n kubeopencode-system -l kubeopencode.io/task=update-deps

# View task logs
kubectl logs job/$(kubectl get task update-deps -o jsonpath='{.status.jobName}') -n kubeopencode-system
```

## Uninstalling the Chart

```bash
helm uninstall kubeopencode --namespace kubeopencode-system
```

To also delete the namespace:

```bash
kubectl delete namespace kubeopencode-system
```

## Security Considerations

1. **Secrets Management**: Never commit secrets to Git. Use:
   - Kubernetes Secrets
   - External Secrets Operator
   - Sealed Secrets
   - HashiCorp Vault

2. **RBAC**: The chart creates minimal RBAC permissions:
   - Controller: Manages CRs and Jobs only

3. **Network Policies**: Consider adding NetworkPolicies to restrict traffic

4. **Pod Security**: Runs with non-root user and dropped capabilities

## Troubleshooting

### Controller not starting

```bash
# Check controller logs
kubectl logs -n kubeopencode-system deployment/kubeopencode-controller

# Check RBAC permissions
kubectl auth can-i create tasks --as=system:serviceaccount:kubeopencode-system:kubeopencode-controller -n kubeopencode-system
```

### Jobs failing

```bash
# List failed Jobs
kubectl get jobs -n kubeopencode-system --field-selector status.successful=0

# Check Job logs
kubectl logs job/<job-name> -n kubeopencode-system

# Describe job for events
kubectl describe job/<job-name> -n kubeopencode-system
```

## Contributing

See the main project [README](../../README.md) for contribution guidelines.

## License

Copyright Contributors to the KubeOpenCode project. Licensed under the Apache License 2.0.
