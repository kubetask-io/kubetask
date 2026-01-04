# ADR 0008: Rebrand KubeTask to KubeOpenCode

## Status

Accepted

## Context

KubeTask was the original name for this Kubernetes-native AI task execution system. As the project evolved, we adopted OpenCode as the primary AI coding tool, replacing multiple agent implementations (Claude, Gemini, Goose, etc.).

Key factors driving the rebranding:

- **OpenCode as Primary Engine**: OpenCode became the sole AI coding tool, providing a unified and powerful interface for AI-assisted development
- **Identity Clarity**: "KubeTask" was generic; "KubeOpenCode" clearly indicates OpenCode integration
- **Ecosystem Alignment**: Better discoverability for OpenCode users seeking Kubernetes integration
- **Simplified Architecture**: Removing multiple AI agents in favor of a unified OpenCode approach reduced maintenance burden and complexity

## Decision

**Rebrand the entire project from KubeTask to KubeOpenCode.**

Key changes:

- API group: `kubetask.io` → `kubeopencode.io`
- Helm chart: `charts/kubetask/` → `charts/kubeopencode/`
- Binary: `cmd/kubetask/` → `cmd/kubeopencode/`
- Namespace: `kubetask-system` → `kubeopencode-system`
- Config CRD: `KubeTaskConfig` → `KubeOpenCodeConfig`
- Container registry: `quay.io/kubetask/` → `quay.io/kubeopencode/`

Agent consolidation:

- Removed: `base`, `claude`, `gemini`, `goose`, `echo` agents
- Kept: `opencode` (init container), `devbox` (executor), `code-server` (IDE)

## Consequences

### Positive

- Clear identity aligned with OpenCode
- Simplified codebase with single AI engine
- Better discoverability in OpenCode ecosystem
- Reduced maintenance burden (fewer agent images to build and maintain)

### Negative

- Breaking change for existing users
- No automatic migration (requires reinstall with new CRDs)
- Existing KubeTask resources become invalid

### Migration

Users must:

1. Uninstall KubeTask (`helm uninstall kubetask`)
2. Delete old CRDs (`kubectl delete crd -l app.kubernetes.io/name=kubetask`)
3. Install KubeOpenCode with new Helm chart
4. Recreate Task/Agent resources with new API group
