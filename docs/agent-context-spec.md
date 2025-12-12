# Agent Context Specification

This document defines how context is mounted and provided to AI agents in Kubernetes Pods.

## Overview

KubeTask uses a Context CRD to provide reusable, shareable context to AI agents during task execution. The Context CRD supports three source types:

1. **Inline**: Content directly in the YAML
2. **ConfigMap**: Reference to a ConfigMap (single key or entire ConfigMap as directory)
3. **Git**: Content from a Git repository (future)

## Context Priority

Contexts are processed in the following priority order (lowest to highest):

1. `Agent.contexts` - Agent-level defaults (referenced Context CRDs)
2. `Task.contexts` - Task-specific contexts (referenced Context CRDs)
3. `Task.description` - Inline task description (becomes start of /workspace/task.md)

Higher priority contexts take precedence. When contexts have empty `mountPath`, they are aggregated into `/workspace/task.md` with XML tags.

## Context CRD Types

### 1. Inline Content

Content is provided directly in the Context YAML:

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Context
metadata:
  name: coding-standards
spec:
  type: Inline
  inline:
    content: |
      # Coding Standards
      - Use descriptive variable names
      - Write unit tests for all functions
      - Follow Go conventions
```

### 2. ConfigMap Key Reference

Content from a specific key in a ConfigMap:

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Context
metadata:
  name: security-policy
spec:
  type: ConfigMap
  configMap:
    name: org-policies
    key: security.md
```

### 3. ConfigMap Directory Mount

When no `key` is specified, the entire ConfigMap is mounted as a directory (requires `mountPath` in ContextMount):

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Context
metadata:
  name: project-configs
spec:
  type: ConfigMap
  configMap:
    name: my-configs  # All keys become files in the directory
```

### 4. Git Repository (Future)

Content from a Git repository:

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Context
metadata:
  name: repo-context
spec:
  type: Git
  git:
    repository: https://github.com/org/contexts
    path: .claude/
    ref: main
```

## ContextMount - How Tasks Reference Contexts

Tasks and Agents reference Contexts using `ContextMount`:

```yaml
contexts:
  - name: coding-standards      # Context name
    namespace: default          # Optional, defaults to Task's namespace
    mountPath: /workspace/guides/standards.md  # Where to mount
```

### Empty MountPath Behavior

When `mountPath` is empty, the context content is appended to `/workspace/task.md` with XML tags:

```xml
<context name="coding-standards" namespace="default" type="Inline">
# Coding Standards
- Use descriptive variable names
...
</context>
```

This enables multiple contexts to be aggregated into a single file that the agent reads.

## Workspace Structure Example

```
/
├── workspace/
│   ├── task.md              # Aggregated: description + contexts without mountPath
│   ├── guides/
│   │   └── standards.md     # Context with explicit mountPath
│   └── configs/             # ConfigMap directory mount
│       ├── config.json
│       └── settings.yaml
└── home/
    └── agent/
        └── .ssh/
            └── id_rsa       # Credential mount
```

## Examples

### Example 1: Simple Task with Description

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: update-deps
spec:
  description: |
    Update all dependencies to latest versions.
    Run tests and create a PR.
  agentRef: gemini
```

Result: `/workspace/task.md` contains the description.

### Example 2: Task with Context References

```yaml
# Reusable Context
apiVersion: kubetask.io/v1alpha1
kind: Context
metadata:
  name: coding-standards
spec:
  type: Inline
  inline:
    content: |
      # Coding Standards
      Follow Go conventions.
---
# Task referencing the Context
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: code-review
spec:
  description: "Review the PR for coding standard violations"
  contexts:
    - name: coding-standards
      mountPath: /workspace/guides/standards.md
  agentRef: claude
```

Result:
- `/workspace/task.md`: Contains the description
- `/workspace/guides/standards.md`: Contains the coding standards

### Example 3: Task with Aggregated Contexts

When contexts don't specify `mountPath`, they are aggregated into task.md:

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: code-review
spec:
  description: "Review this PR"
  contexts:
    - name: coding-standards
      # No mountPath - will be appended to task.md
    - name: security-policy
      # No mountPath - will be appended to task.md
  agentRef: claude
```

Result `/workspace/task.md`:
```markdown
Review this PR

<context name="coding-standards" namespace="default" type="Inline">
# Coding Standards
Follow Go conventions.
</context>

<context name="security-policy" namespace="default" type="ConfigMap">
# Security Policy
All code must be reviewed.
</context>
```

### Example 4: Agent with Default Contexts

Agent provides organization-wide defaults that are merged with Task contexts:

```yaml
apiVersion: kubetask.io/v1alpha1
kind: Agent
metadata:
  name: default
spec:
  agentImage: quay.io/myorg/claude-agent:v1.0
  serviceAccountName: kubetask-agent
  contexts:
    # Organization coding standards - applied to all tasks
    - name: org-coding-standards
      # No mountPath - appended to task.md
    - name: org-security-policy
      # No mountPath - appended to task.md
---
apiVersion: kubetask.io/v1alpha1
kind: Task
metadata:
  name: update-service
spec:
  description: "Update dependencies and create PR"
  agentRef: default
```

Result: `/workspace/task.md` contains:
1. Task description (highest priority)
2. org-coding-standards content (from Agent)
3. org-security-policy content (from Agent)

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
  serviceAccountName: kubetask-agent
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

1. **Read `/workspace/task.md`** as the primary task description
2. **Parse XML context tags** when multiple contexts are aggregated
3. **Check for additional files** at explicitly mounted paths
4. **Use credentials securely**: Never log or expose credential values

### Environment Variables

The controller provides these environment variables to the agent:

| Variable | Description |
|----------|-------------|
| `TASK_NAME` | Name of the Task CR |
| `TASK_NAMESPACE` | Namespace of the Task CR |
| `KUBETASK_KEEP_ALIVE_SECONDS` | (if humanInTheLoop enabled) Keep-alive duration |
| `GITHUB_TOKEN` | (if configured) GitHub API token |
| `ANTHROPIC_API_KEY` | (if configured) Anthropic API key |
| ... | Other credentials as configured in Agent |

### Recommended Agent Behavior

```
1. Read /workspace/task.md to understand the task
2. Parse any XML context tags for additional context
3. Check for additional configuration files at mounted paths
4. Execute the task as described
5. Exit with code 0 on success, non-zero on failure
```

## Summary

| Context Type | Source | Description |
|--------------|--------|-------------|
| `Inline` | `spec.inline.content` | Content directly in YAML |
| `ConfigMap` | `spec.configMap.name + key` | Single file from ConfigMap key |
| `ConfigMap` | `spec.configMap.name` (no key) | Directory mount with all ConfigMap keys |
| `Git` | `spec.git.repository + path` | Content from Git repository (future) |

| Priority | Context Source | Description |
|----------|---------------|-------------|
| Lowest | `Agent.contexts` | Agent-level defaults (Context CRD refs) |
| Middle | `Task.contexts` | Task-specific (Context CRD refs) |
| Highest | `Task.description` | Inline description (becomes /workspace/task.md) |
