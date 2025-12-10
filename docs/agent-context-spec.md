# Agent Context Specification

This document defines how context items from Task/Batch are mounted and provided to AI agents in Kubernetes Pods.

## Overview

Context items provide information to AI agents during task execution. KubeTask supports two modes for delivering context to agents:

1. **Aggregated Mode (Default)**: Contexts without `mountPath` are combined into a single file
2. **Explicit Mount Mode**: Contexts with `mountPath` are mounted at the specified location

## Context Priority

Contexts are processed in the following priority order (lowest to highest):

1. `Agent.defaultContexts` - Base layer (organization-wide defaults)
2. `Batch.commonContext` - Shared across all tasks in the batch
3. `Batch.variableContexts[i]` - Task-specific contexts

Higher priority contexts are processed after lower priority ones. When aggregated, this means they appear later in the file.

## Default Aggregation

When a `FileContext` does not specify a `mountPath`, its content is aggregated into:

```
/workspace/task.md
```

### Aggregation Format

Multiple contexts are concatenated with markdown horizontal rules (`---`) as separators:

```markdown
[Content from Agent.defaultContexts[0]]

---

[Content from Agent.defaultContexts[1]]

---

[Content from Batch.commonContext[0]]

---

[Content from Batch.variableContexts[i][0]]
```

## Explicit Mount Path

When a `FileContext` specifies a `mountPath`, the file is mounted at that exact location in the pod filesystem.

### API Definition

```yaml
type: File
file:
  name: config.json
  source:
    inline: |
      {"key": "value"}
  mountPath: /etc/myapp/config.json  # Optional: explicit mount location
```

### Mount Path Guidelines

- Can be any valid absolute path in the container filesystem
- No reserved paths - all locations are available for mounting
- Agent must handle both aggregated and explicitly mounted contexts

## Workspace Structure Example

```
/
├── workspace/
│   └── task.md              # Aggregated context (default)
├── etc/
│   └── myapp/
│       └── config.json      # Explicit: mountPath="/etc/myapp/config.json"
└── home/
    └── agent/
        └── .claude/
            └── CLAUDE.md    # Explicit: mountPath="/home/agent/.claude/CLAUDE.md"
```

## Examples

### Example 1: Basic Task with Aggregated Context

All contexts are aggregated into `/workspace/task.md`:

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: update-deps
spec:
  contexts:
    - type: File
      file:
        name: task.md
        source:
          inline: |
            Update all dependencies to latest versions.
            Run tests and create a PR.
    - type: File
      file:
        name: guide.md
        source:
          configMapKeyRef:
            name: workflow-guides
            key: pr-workflow.md
```

Result in `/workspace/task.md`:
```markdown
Update all dependencies to latest versions.
Run tests and create a PR.

---

[Content of pr-workflow.md from ConfigMap]
```

### Example 2: Mixed Contexts (Aggregated + Explicit)

Some contexts aggregated, others at specific paths:

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: deploy-service
spec:
  contexts:
    # Aggregated into /workspace/task.md
    - type: File
      file:
        name: task.md
        source:
          inline: |
            Deploy the service to production.

    # Explicitly mounted at specific path
    - type: File
      file:
        name: CLAUDE.md
        source:
          configMapKeyRef:
            name: agent-configs
            key: claude-instructions.md
        mountPath: /home/agent/.claude/CLAUDE.md

    # Explicitly mounted configuration
    - type: File
      file:
        name: deploy-config.yaml
        source:
          secretKeyRef:
            name: deploy-secrets
            key: production.yaml
        mountPath: /etc/deploy/config.yaml
```

### Example 3: Batch with Agent Defaults

Agent provides organization-wide defaults:

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: default
spec:
  agentImage: quay.io/myorg/claude-agent:v1.0
  defaultContexts:
    # Organization coding standards - aggregated
    - type: File
      file:
        name: coding-standards.md
        source:
          configMapKeyRef:
            name: org-standards
            key: coding.md

    # Claude configuration - explicit path
    - type: File
      file:
        name: CLAUDE.md
        source:
          configMapKeyRef:
            name: org-standards
            key: claude-config.md
        mountPath: /home/agent/.claude/CLAUDE.md
---
apiVersion: kubetask.io/v1alpha1
kind: Batch
metadata:
  name: update-all-repos
spec:
  commonContext:
    - type: File
      file:
        name: task.md
        source:
          inline: |
            Update dependencies and create PR.
  variableContexts:
    - - type: File
        file:
          name: repo-config.json
          source:
            configMapKeyRef:
              name: repo-configs
              key: service-a.json
          mountPath: /workspace/repo-config.json
```

Result for each task:
- `/workspace/task.md`: Contains coding-standards.md + task.md (aggregated)
- `/home/agent/.claude/CLAUDE.md`: Claude configuration (from Agent)
- `/workspace/repo-config.json`: Repository-specific config (from variableContexts)

## Credentials

Agent supports credentials for providing secrets to agents. Credentials can be exposed as:
- **Environment Variables**: For API tokens, passwords, etc.
- **File Mounts**: For SSH keys, service account files, etc.

### Credential Configuration

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: default
spec:
  agentImage: quay.io/myorg/claude-agent:v1.0
  credentials:
    # GitHub token as environment variable
    - name: github-token
      secretRef:
        name: github-credentials
        key: token
      env: GITHUB_TOKEN

    # SSH key as file mount
    - name: ssh-key
      secretRef:
        name: ssh-credentials
        key: id_rsa
      mountPath: /home/agent/.ssh/id_rsa
      fileMode: 0400  # Read-only for SSH keys

    # Anthropic API key as environment variable
    - name: anthropic-api-key
      secretRef:
        name: ai-credentials
        key: anthropic-key
      env: ANTHROPIC_API_KEY

    # GCP service account as file mount
    - name: gcp-credentials
      secretRef:
        name: gcp-sa
        key: credentials.json
      mountPath: /home/agent/.config/gcloud/application_default_credentials.json
      fileMode: 0600
```

### Credential Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Descriptive name for documentation |
| `secretRef.name` | Yes | Name of the Kubernetes Secret |
| `secretRef.key` | Yes | Key within the Secret to use |
| `env` | No | Environment variable name to expose the secret |
| `mountPath` | No | File path to mount the secret |
| `fileMode` | No | File permission mode (default: 0600) |

A credential can have both `env` and `mountPath` specified to expose the same secret value in both ways.

### Security Best Practices

1. **Use restrictive file modes**: Default is `0600` (read/write owner only). Use `0400` for read-only files like SSH keys.
2. **Avoid logging secrets**: Agents should never log secret values.
3. **Use short-lived tokens**: Prefer short-lived tokens over long-lived credentials.
4. **Principle of least privilege**: Only include credentials that are actually needed.

## Agent Implementation Guide

Agents should:

1. **Always check `/workspace/task.md`** for the main task description
2. **Handle additional mounted files** as specified in their documentation
3. **Not assume any specific file structure** beyond `/workspace/task.md`
4. **Use credentials securely**: Never log or expose credential values

### Environment Variables

The controller provides these environment variables to the agent:

| Variable | Description |
|----------|-------------|
| `TASK_NAME` | Name of the Task CR |
| `TASK_NAMESPACE` | Namespace of the Task CR |
| `GITHUB_TOKEN` | (if configured) GitHub API token |
| `ANTHROPIC_API_KEY` | (if configured) Anthropic API key |
| ... | Other credentials as configured in Agent |

### Recommended Agent Behavior

```
1. Read /workspace/task.md to understand the task
2. Check for any explicitly mounted configuration files
3. Execute the task as described
4. Report results via Task CR status updates
```

## Summary

| Scenario | Mount Location |
|----------|----------------|
| No `mountPath` specified | Aggregated into `/workspace/task.md` |
| `mountPath` specified | Mounted at the specified path |

| Priority | Context Source | Description |
|----------|---------------|-------------|
| Lowest | `Agent.defaultContexts` | Organization defaults |
| Medium | `Batch.commonContext` | Batch-wide shared context |
| Highest | `Batch.variableContexts[i]` | Task-specific context |
