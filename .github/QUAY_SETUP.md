# Quay.io Setup for GitHub Actions

## 1. Create Robot Account on Quay.io (Organization Level)

1. Login to [quay.io](https://quay.io)
2. Go to **Organizations** → **kubetask** → **Robot Accounts** → **Create Robot Account**
3. Name it (e.g., `github_actions`)
4. Grant **Write** permission to all repositories:
   - `kubetask/kubetask-controller`
   - `kubetask/kubetask-agent-base`
   - `kubetask/kubetask-agent-gemini`
   - `kubetask/kubetask-agent-echo`
   - `kubetask/kubetask-agent-goose`

## 2. Add GitHub Secrets

Go to GitHub org → **Settings** → **Secrets and variables** → **Actions** → **New organization secret**:

| Name | Value |
|------|-------|
| `QUAY_USERNAME` | `kubetask+github_actions` |
| `QUAY_TOKEN` | Robot token from step 1 |

**Note**: Using org-level secrets allows all repositories under the org to share the same credentials.

## 3. Ensure Repositories Exist

Create repos on Quay.io or enable **Auto-create repositories** in organization settings.
