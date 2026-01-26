# KubeOpenCode Slack Integration

This directory contains the Slack Bot integration for KubeOpenCode. The bot allows users to interact with KubeOpenCode Agents directly from Slack channels.

## Architecture

```
┌──────────────┐  WebSocket  ┌─────────────────────┐  K8s API  ┌───────────────┐
│    Slack     │◀───────────▶│  Socket Mode        │──────────▶│  KubeOpenCode │
│    Server    │  (outbound) │  Gateway (Pod)      │           │  Task         │
└──────────────┘             └─────────────────────┘           └───────────────┘
```

**Key Features:**
- No public ingress required (outbound WebSocket connection only)
- No custom Docker image needed (uses standard `python:3.12-slim`)
- Direct Task creation with immediate Slack acknowledgment
- Simple deployment via single YAML file

## Quick Start

### 1. Configure Slack App

1. Go to [Slack API Apps](https://api.slack.com/apps) and create/select your app

2. **Enable Socket Mode:**
   - Navigate to **Socket Mode**
   - Toggle **Enable Socket Mode** to ON

3. **Generate App-Level Token:**
   - Go to **Basic Information** → **App-Level Tokens**
   - Click **Generate Token and Scopes**
   - Name: `socket-mode`
   - Add scope: `connections:write`
   - Click **Generate** and copy the token (`xapp-...`)

4. **Configure Bot Token Scopes:**
   - Go to **OAuth & Permissions** → **Scopes** → **Bot Token Scopes**
   - Add: `app_mentions:read`, `chat:write`
   - Optional: `files:write` (for file uploads)

5. **Subscribe to Events:**
   - Go to **Event Subscriptions**
   - Enable Events
   - Under **Subscribe to bot events**, add:
     - `app_mention`
     - `message.im` (optional, for DMs)

6. **Install App:**
   - Go to **Install App** and install to your workspace
   - Copy the **Bot User OAuth Token** (`xoxb-...`)

### 2. Create Secret

```bash
kubectl create namespace kubeopencode-slack

kubectl create secret generic slack-socket-mode-creds \
  --from-literal=app-token=xapp-YOUR-APP-LEVEL-TOKEN \
  --from-literal=bot-token=xoxb-YOUR-BOT-TOKEN \
  -n kubeopencode-slack
```

### 3. Deploy

```bash
# Option A: Using kustomize
kubectl apply -k deploy/dogfooding/slack/

# Option B: Direct apply
kubectl apply -f deploy/dogfooding/slack/namespace.yaml
kubectl apply -f deploy/dogfooding/slack/socket-mode-gateway.yaml -n kubeopencode-slack
```

### 4. Verify

```bash
# Check logs
kubectl logs -f deployment/slack-socket-mode-gateway -n kubeopencode-slack

# Expected output:
# Starting Socket Mode Gateway...
# Task namespace: kubeopencode-dogfooding, Agent: dev-agent
```

### 5. Test

In Slack, mention your bot:
```
@YourBot help me fix the authentication bug in login.js
```

The bot will:
1. Acknowledge immediately with "🤖 Got it! Creating task for..."
2. Create a KubeOpenCode Task
3. Reply with "✅ Task created: `slack-20260126-...`"

## Configuration

Edit `socket-mode-gateway.yaml` to customize:

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `TASK_NAMESPACE` | `kubeopencode-dogfooding` | Namespace for created Tasks |
| `AGENT_NAME` | `dev-agent` | Agent to use for Tasks |
| `AGENT_NAMESPACE` | `kubeopencode-dogfooding` | Namespace of the Agent |

## How It Works

1. **Socket Mode Gateway** connects to Slack via WebSocket (outbound)
2. When bot is @mentioned, Slack sends event via WebSocket
3. Gateway extracts message text and creates a KubeOpenCode Task
4. Gateway replies in the same thread with task status
5. Task runs using the configured Agent
6. Agent can use `slack-cli` to send results back (see devbox image)

## Files

| File | Description |
|------|-------------|
| `socket-mode-gateway.yaml` | Main deployment (ConfigMap + RBAC + Deployment) |
| `namespace.yaml` | Namespace definition |
| `kustomization.yaml` | Kustomize configuration |

## Troubleshooting

### Gateway not starting
```bash
kubectl describe pod -l app=slack-socket-mode-gateway -n kubeopencode-slack
```

### Connection issues
Check that:
1. `SLACK_APP_TOKEN` starts with `xapp-`
2. `SLACK_BOT_TOKEN` starts with `xoxb-`
3. Socket Mode is enabled in Slack App settings

### Task not created
Check RBAC:
```bash
kubectl auth can-i create tasks.kubeopencode.io \
  --as=system:serviceaccount:kubeopencode-slack:slack-socket-mode-gateway
```

