# Quay.io Setup for GitHub Actions

## 1. Create Robot Account on Quay.io

1. Login to [quay.io](https://quay.io)
2. Go to **Account Settings** → **Robot Accounts** → **Create Robot Account**
3. Name it (e.g., `github_actions`)
4. Grant **Write** permission to:
   - `zhaoxue/kubetask-controller`
   - `zhaoxue/kubetask-agent-gemini`
   - `zhaoxue/kubetask-agent-echo`

## 2. Add GitHub Secrets

Go to GitHub repo → **Settings** → **Secrets and variables** → **Actions** → **New repository secret**:

| Name | Value |
|------|-------|
| `QUAY_USERNAME` | `zhaoxue+github_actions` |
| `QUAY_PASSWORD` | Robot token from step 1 |

## 3. Ensure Repositories Exist

Create repos on Quay.io or enable **Auto-create repositories** in user settings.
