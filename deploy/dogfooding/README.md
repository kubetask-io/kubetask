# KubeOpenCode Dogfooding Environment

This directory contains resources for running KubeOpenCode in a dogfooding environment, where KubeOpenCode is used to automate tasks on its own repository.

## Architecture

```
GitHub (kubeopencode/kubeopencode)
    │
    │ Webhook events (PR opened, issue comments, etc.)
    │
    ▼
smee.io (https://smee.io/YOUR_CHANNEL_ID)
    │
    │ Forwards webhooks (trusted SSL)
    │
    ▼
smee-client (Pod in kubeopencode-system)
    │
    │ HTTP to internal service
    │
    ▼
kubeopencode-webhook (Service in kubeopencode-system)
    │
    │ Matches rules, creates Tasks
    │
    ▼
WebhookTrigger (kubeopencode-dogfooding/github)
    │
    ▼
Task → Job → Agent Pod
```

## Why smee.io?

GitHub webhooks require endpoints with SSL certificates signed by trusted public CAs. OpenShift's default ingress uses self-signed certificates, which GitHub rejects with:

```
tls: failed to verify certificate: x509: certificate signed by unknown authority
```

[smee.io](https://smee.io) is GitHub's recommended solution for development/testing. It provides a public HTTPS endpoint with trusted certificates and forwards webhooks to your internal services.

## Directory Structure

```
deploy/dogfooding/
├── README.md                 # This file
├── base/                     # Resources for kubeopencode-dogfooding namespace
│   ├── kustomization.yaml
│   ├── namespace.yaml
│   ├── rbac.yaml
│   ├── secrets.yaml          # Contains github-webhook-secret
│   ├── agent-default.yaml    # Default Agent configuration
│   └── context-*.yaml        # Context resources
├── system/                   # Resources for kubeopencode-system namespace
│   ├── kustomization.yaml
│   ├── deployment-smee-client.yaml  # Smee.io webhook proxy
│   └── route-webhook.yaml    # OpenShift Route (optional)
├── resources/                # WebhookTrigger definitions
│   └── webhooktrigger-github.yaml
└── examples/                 # Example Tasks
```

## Setup

### Prerequisites

1. KubeOpenCode installed in `kubeopencode-system` namespace with webhook enabled:
   ```bash
   helm install kubeopencode ./charts/kubeopencode \
     --namespace kubeopencode-system \
     --set webhook.enabled=true
   ```

2. A GitHub App configured for the repository (see [GitHub App Setup](#github-app-setup))

### Deploy Dogfooding Resources

```bash
# Apply base resources (kubeopencode-dogfooding namespace)
kubectl apply -k deploy/dogfooding/base

# Apply system resources (kubeopencode-system namespace)
kubectl apply -k deploy/dogfooding/system

# Apply WebhookTrigger
kubectl apply -f deploy/dogfooding/resources/webhooktrigger-github.yaml
```

### Verify Deployment

```bash
# Check smee-client is running
kubectl get pods -n kubeopencode-system -l app.kubernetes.io/name=smee-client

# Check smee-client logs
kubectl logs -n kubeopencode-system -l app.kubernetes.io/name=smee-client

# Check webhook server registered the trigger
kubectl logs -n kubeopencode-system -l app.kubernetes.io/component=webhook | grep "Registered"
```

## GitHub App Setup

### 1. Create a GitHub App

1. Go to GitHub Settings → Developer settings → GitHub Apps → New GitHub App
2. Configure:
   - **App name**: `kubeopencode-bot`
   - **Homepage URL**: `https://github.com/kubeopencode/kubeopencode`
   - **Webhook URL**: `https://smee.io/YOUR_CHANNEL_ID` (from your smee.io channel)
   - **Webhook secret**: Same as `hmacKey` in `github-webhook-secret`
   - **Permissions**:
     - Repository: Contents (Read & Write), Issues (Read & Write), Pull requests (Read & Write)
   - **Subscribe to events**: Issue comment, Pull request

### 2. Install the App

Install the GitHub App on the `kubeopencode/kubeopencode` repository.

### 3. Configure Secrets

Create the webhook secret:
```bash
kubectl create secret generic github-webhook-secret \
  --namespace kubeopencode-dogfooding \
  --from-literal=hmacKey=<your-webhook-secret>
```

## Changing the smee.io Channel

If you need to create a new smee.io channel:

1. Go to https://smee.io/ and click "Start a new channel"
2. Update `system/deployment-smee-client.yaml` with the new URL
3. Update the GitHub App's Webhook URL
4. Re-apply the deployment:
   ```bash
   kubectl apply -k deploy/dogfooding/system
   ```

## WebhookTrigger Rules

The `github` WebhookTrigger in `resources/webhooktrigger-github.yaml` defines:

| Rule | Event | Trigger Condition |
|------|-------|-------------------|
| `pr-opened` | `pull_request` | PR is opened |
| `comment-privileged` | `issue_comment` | `@kubeopencode-bot` mention from OWNER/MEMBER/CONTRIBUTOR/COLLABORATOR |
| `comment-unprivileged` | `issue_comment` | `@kubeopencode-bot` mention from other users |

## Troubleshooting

### Webhook not triggering

1. **Check smee-client logs**:
   ```bash
   kubectl logs -n kubeopencode-system -l app.kubernetes.io/name=smee-client -f
   ```

2. **Check webhook server logs**:
   ```bash
   kubectl logs -n kubeopencode-system -l app.kubernetes.io/component=webhook -f
   ```

3. **Check GitHub App delivery history**:
   Go to GitHub App settings → Advanced → Recent Deliveries

### Authentication failed

If you see `Authentication failed` in webhook logs:
- Verify the `hmacKey` in `github-webhook-secret` matches the GitHub App's webhook secret
- Ensure the secret is in the correct namespace (`kubeopencode-dogfooding`)

### No Tasks created

Check the WebhookTrigger status:
```bash
kubectl get webhooktrigger -n kubeopencode-dogfooding github -o yaml
```

Check if the filter conditions match your event.

## Production Considerations

For production environments, consider:

1. **Use a trusted SSL certificate** instead of smee.io:
   - Configure Let's Encrypt with cert-manager
   - Or use a commercial SSL certificate

2. **Use a dedicated Route/Ingress** with proper TLS:
   ```yaml
   spec:
     tls:
       termination: edge
       certificate: <your-cert>
       key: <your-key>
   ```

3. **Secure the webhook secret** using external secret management (e.g., Vault, Sealed Secrets)
