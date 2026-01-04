# ADR 0006: Environment Configuration for Agent Containers in SCC Environments

## Status

Accepted

## Context

KubeOpenCode agent containers run AI CLI tools (gemini-cli, claude-cli, etc.) that require write access to their home directory. These tools create configuration and cache directories like `~/.gemini`, `~/.claude`, etc.

When running on Kubernetes clusters with Security Context Constraints (SCC) or similar security policies (e.g., OpenShift, hardened clusters), containers are forced to run with random UIDs that have no entry in `/etc/passwd`. This causes several issues:

### Problem 1: HOME defaults to root directory

When a container runs as a random UID (e.g., 1000730000), the system cannot find a matching entry in `/etc/passwd`, so:
- `$HOME` defaults to `/` (root directory)
- `/` is read-only, so tools fail when trying to create `~/.gemini`

### Problem 2: Image-defined HOME is not writable

Even if the Dockerfile defines `USER agent` with `HOME=/home/agent`:
- SCC overrides the UID to a random value
- `/home/agent` is owned by the original UID (e.g., 1000)
- The random UID cannot write to `/home/agent`

### Problem 3: Workspace directory ownership

The workspace directory (`/workspace`) is created during image build and owned by the image user:
- In Dockerfile: `chown agent:agent /workspace`
- At runtime: owned by UID 1000 with permissions 755
- Random UID cannot write to `/workspace` directly

### Problem 4: SHELL defaults to /sbin/nologin

Some SCC environments inject a passwd entry for the random UID with `/sbin/nologin` as the shell:
```
1000730000:x:1000730000:0:1000730000 user:/tmp:/sbin/nologin
```

This causes terminals in tools like code-server to fail with:
```
The terminal process "/sbin/nologin" terminated with exit code: 1.
```

### Environment Comparison

| Environment | Container UID | HOME | SHELL | Issues |
|-------------|---------------|------|-------|--------|
| Kind/vanilla K8s | 1000 (agent) | /home/agent | /bin/zsh | None |
| SCC-enabled cluster | Random (e.g., 1000730000) | / | /sbin/nologin | HOME not writable, terminal fails |

## Decision

We set `HOME=/tmp` and `SHELL=/bin/bash` for all agent containers via environment variables in the controller's `job_builder.go`.

```go
envVars = append(envVars,
    corev1.EnvVar{Name: "HOME", Value: "/tmp"},
    corev1.EnvVar{Name: "SHELL", Value: "/bin/bash"},
    // ... other env vars
)
```

### Why `HOME=/tmp`?

1. **Always writable**: `/tmp` has permissions `1777` (drwxrwxrwt), allowing any user to write
2. **Standard location**: Well-known temporary directory on all Linux systems
3. **No ownership issues**: Works regardless of container UID
4. **Ephemeral by design**: Suitable for temporary config/cache files

### Why `SHELL=/bin/bash`?

1. **Overrides /sbin/nologin**: Ensures terminals work in tools like code-server
2. **Available in all images**: `/bin/bash` is present in our agent base images
3. **Standard shell**: Compatible with most scripts and tools

### Why controller-level vs Dockerfile-level?

| Approach | Pros | Cons |
|----------|------|------|
| Controller | Single change benefits all agents; works with third-party images | Agents depend on controller behavior |
| Dockerfile | Image is self-contained | Must update all agent images; doesn't help third-party images |

We chose the controller approach because:
1. **Universal fix**: All agent images automatically benefit
2. **Third-party support**: Works with user-provided agent images
3. **Centralized control**: Single point of configuration

## Consequences

### Positive

- Agent containers work correctly on both vanilla Kubernetes and SCC-enabled clusters
- AI CLI tools can create their config directories (`~/.gemini`, `~/.claude`, etc.)
- Terminals in code-server and similar tools work correctly
- No changes required to agent Dockerfiles
- Third-party agent images work without modification

### Negative

- Agent home directory is ephemeral (lost when container restarts)
- CLI tool configurations are not persisted across task runs
- `/tmp` may have size limits on some systems

### Mitigations

- For persistent configuration, use volume mounts to specific paths
- CLI tool state is typically not needed across tasks (each task is independent)
- If `/tmp` size is a concern, consider mounting an emptyDir volume at `/tmp`

## References

- [OpenShift SCC Documentation](https://docs.openshift.com/container-platform/latest/authentication/managing-security-context-constraints.html)
- [Kubernetes Security Context](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/)
- [Random UID in OpenShift](https://docs.openshift.com/container-platform/latest/openshift_images/create-images.html#images-create-guide-openshift_create-images)
