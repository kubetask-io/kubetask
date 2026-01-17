# ADR 0010: OpenCode Permission Configuration for Automated Execution

## Status

Accepted

## Context

KubeOpenCode runs OpenCode CLI in Kubernetes Pods to execute AI-powered tasks. OpenCode has a permission system that prompts users for approval before executing certain operations (bash commands, file edits, external directory access, etc.).

### The Problem

When running in a Pod environment, OpenCode's permission prompts block task execution:

```
◆  Permission required: external_directory (/tmp/git-config)
│  ● Allow once
│  ○ Always allow: /tmp*
│  ○ Reject
```

This is problematic because:
1. **Non-interactive environment**: Kubernetes Pods have no TTY for user input
2. **Task execution blocks indefinitely**: The Pod hangs waiting for permission approval
3. **Automation breaks**: CI/CD pipelines cannot proceed without human intervention

### OpenCode Permission System

OpenCode supports three permission modes for each tool:
- `"ask"` — Prompt for approval (default for most tools)
- `"allow"` — Allow without prompting
- `"deny"` — Deny and block the operation

Configurable tools include:
- `bash` — Shell command execution
- `edit` — File editing operations
- `read`, `glob`, `grep`, `list` — File system operations
- `external_directory` — Access to directories outside workspace
- `webfetch`, `websearch` — Network operations
- `doom_loop` — Agentic iteration control
- And more...

### Configuration Methods

OpenCode supports two ways to configure permissions:

1. **Configuration file** (`opencode.json`):
```json
{
  "permission": {
    "bash": "allow",
    "edit": "allow",
    "external_directory": "allow"
  }
}
```

2. **Environment variable** (`OPENCODE_PERMISSION`):
```bash
OPENCODE_PERMISSION='{"bash":"allow","edit":"allow"}' opencode run "task"
```

### Merge Behavior

When both config file and environment variable are set, OpenCode performs a **deep merge**:

```typescript
// From opencode/packages/opencode/src/config/config.ts:123-125
if (Flag.OPENCODE_PERMISSION) {
  result.permission = mergeDeep(result.permission ?? {}, JSON.parse(Flag.OPENCODE_PERMISSION))
}
```

**Priority order** (higher takes precedence):
1. Config file `permission` field (highest)
2. `OPENCODE_PERMISSION` environment variable (lower)

This means config file settings override environment variable settings for the same field.

### Shorthand Syntax (Config File Only)

OpenCode supports a shorthand in **config files** where a single action applies to all tools:

```typescript
// From opencode/packages/opencode/src/config/config.ts:414-416
.or(PermissionAction)
.transform((x) => (typeof x === "string" ? { "*": x } : x))
```

So in config files, `"allow"` is automatically transformed to `{"*": "allow"}`.

**Important**: This shorthand does NOT work for the `OPENCODE_PERMISSION` environment variable
because the value is parsed directly with `JSON.parse()` before the transform is applied.
The environment variable must be valid JSON: `{"*":"allow"}`.

## Decision

We inject `OPENCODE_PERMISSION={"*":"allow"}` environment variable in all Task Pods to enable fully automated execution.

### Implementation

In `internal/controller/pod_builder.go`:

```go
const (
    // OpenCodePermissionEnvVar is the environment variable for OpenCode permission configuration.
    OpenCodePermissionEnvVar = "OPENCODE_PERMISSION"

    // DefaultOpenCodePermission enables all permissions for automated execution.
    // Must be valid JSON since OpenCode parses it with JSON.parse().
    DefaultOpenCodePermission = `{"*":"allow"}`
)

// In buildPod function:
envVars = append(envVars, corev1.EnvVar{
    Name:  OpenCodePermissionEnvVar,
    Value: DefaultOpenCodePermission,
})
```

### Why Environment Variable Instead of Config File?

| Approach | Pros | Cons |
|----------|------|------|
| Environment variable | Simple, no file I/O, works with any agent image | Lower priority than config file |
| Config file | Higher priority | Requires writing file, more complex |

We chose environment variable because:
1. **Simpler implementation**: No need to manage file paths or merge with user config
2. **User override works naturally**: Users can set stricter permissions in `Agent.spec.config`, which takes precedence
3. **Defense in depth**: Secure defaults can be overridden for specific use cases

### How Users Can Restrict Permissions

If users want to restrict permissions for security, they can configure in `Agent.spec.config`:

```yaml
apiVersion: kubeopencode.io/v1alpha1
kind: Agent
metadata:
  name: restricted-agent
spec:
  workspaceDir: /workspace
  serviceAccountName: kubeopencode-agent
  config: |
    {
      "permission": {
        "bash": {
          "git *": "allow",
          "npm *": "allow",
          "*": "deny"
        },
        "external_directory": "deny",
        "webfetch": "deny"
      }
    }
```

This config will **override** the default `OPENCODE_PERMISSION="allow"` for the specified tools.

## Consequences

### Positive

- Task Pods execute without permission prompts blocking execution
- Full autonomous operation in Kubernetes/CI environments
- Users can still restrict permissions via `Agent.spec.config` when needed
- Simple implementation with minimal code changes
- Works with any OpenCode version that supports the permission system

### Negative

- All permissions are allowed by default (security consideration)
- AI agents can execute any bash command, edit any file, access external directories
- Users must explicitly configure restrictions for sensitive environments

### Mitigations

1. **Kubernetes RBAC**: Limit what the ServiceAccount can do in the cluster
2. **Network Policies**: Restrict network access from agent Pods
3. **Runtime Class**: Use gVisor or Kata for additional isolation
4. **Agent-level config**: Configure restrictive permissions in `Agent.spec.config`
5. **Workspace isolation**: Agent only has access to mounted volumes

### Security Recommendations

For production environments with sensitive data:

```yaml
apiVersion: kubeopencode.io/v1alpha1
kind: Agent
metadata:
  name: secure-agent
spec:
  workspaceDir: /workspace
  serviceAccountName: restricted-sa  # Limited RBAC
  podSpec:
    runtimeClassName: gvisor  # Sandboxed runtime
  config: |
    {
      "permission": {
        "bash": {
          "git *": "allow",
          "npm run *": "allow",
          "*": "deny"
        },
        "external_directory": "deny"
      }
    }
```

## References

- [OpenCode Permission Documentation](https://opencode.ai/docs/permissions)
- [OpenCode Config Schema](https://opencode.ai/config.json)
- OpenCode source: `packages/opencode/src/config/config.ts`
- OpenCode source: `packages/opencode/src/permission/next.ts`
- KubeOpenCode implementation: `internal/controller/pod_builder.go`
