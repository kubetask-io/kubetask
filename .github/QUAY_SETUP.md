# Quay.io Setup for GitHub Actions

## 1. Create Robot Account on Quay.io (Organization Level)

1. Login to [quay.io](https://quay.io)
2. Go to **Organizations** → **kubeopencode** → **Robot Accounts** → **Create Robot Account**
3. Name it (e.g., `github_actions`)
4. Grant **Write** permission to all repositories:
   - `kubeopencode/kubeopencode` (unified binary: controller, git-init, context-init)
   - `kubeopencode/kubeopencode-agent-opencode` (OpenCode CLI init container)
   - `kubeopencode/kubeopencode-agent-devbox` (Universal development environment)
   - `kubeopencode/helm-charts/kubeopencode` (Helm chart OCI)

## 2. Add GitHub Secrets

Go to GitHub org → **Settings** → **Secrets and variables** → **Actions** → **New organization secret**:

| Name | Value |
|------|-------|
| `QUAY_ROBOT_ACCOUNT` | `kubeopencode+github_actions` |
| `QUAY_TOKEN` | Robot token from step 1 |

**Note**: Using org-level secrets allows all repositories under the org to share the same credentials.

## 3. Ensure Repositories Exist

Create repos on Quay.io or enable **Auto-create repositories** in organization settings.
