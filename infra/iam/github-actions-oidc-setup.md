# GitHub Actions → AWS OIDC trust setup

One-time manual setup so the `deploy.yml` workflow can authenticate to AWS without storing long-lived access keys in GitHub.

## Why OIDC

GitHub Actions issues a short-lived OpenID Connect token on every workflow run. AWS verifies that token against a trusted Identity Provider, then issues temporary credentials via `sts:AssumeRoleWithWebIdentity`. No persistent secret in GitHub. Trust is scoped to a specific repo and branch — a workflow on a different branch (or different repo) cannot assume the role.

## Steps

### 1. Create the OIDC Identity Provider (one-time per AWS account)

Console path: **IAM → Identity providers → Add provider**

- Provider type: **OpenID Connect**
- Provider URL: `https://token.actions.githubusercontent.com`
- Audience: `sts.amazonaws.com`

Click **Add provider**. AWS auto-fetches GitHub's OIDC thumbprints.

### 2. Create the deploy role

Console path: **IAM → Roles → Create role**

- Trusted entity type: **Web identity**
- Identity provider: `token.actions.githubusercontent.com`
- Audience: `sts.amazonaws.com`
- GitHub organization: `wheresdiasd`
- GitHub repository: `safe-upload-platform`
- GitHub branch: `main`

This produces a default trust policy. **Replace it** with the contents of `github-actions-deploy-trust-policy.json` (substitute `<AWS_ACCOUNT_ID>` with `580909335056`). The reason to replace: the console-generated trust policy is sometimes overly permissive (allows any branch, any PR). The version here is exact-match for `main` only.

### 3. Skip the "Add permissions" step (yes, really)

Step 2 of role creation only attaches **existing** managed/customer policies. The JSON in `github-actions-deploy-policy.json` becomes an **inline** policy on this specific role, and AWS only lets you create inline policies *after* the role exists. So:

- Don't select anything from the managed policy list.
- Click **Next** to advance to step 3.

### 4. Name and create the role

- Role name: **`safe-upload-github-deploy`**
- Click **Create role**.
- Capture the resulting role ARN — you'll need it for the GitHub repo variable.

### 5. Attach the inline permission policy

Now the role exists. Open it from IAM → Roles → `safe-upload-github-deploy`.

- **Permissions** tab → **Add permissions ▼ → Create inline policy**.
- Click the **JSON** tab (top-right of the editor).
- Paste the contents of `github-actions-deploy-policy.json` and substitute placeholders:
  - `<AWS_REGION>` → `eu-west-2`
  - `<AWS_ACCOUNT_ID>` → `580909335056`
- **Next** → policy name `safe-upload-deploy-lambda-update` → **Create policy**.

### 6. Configure the GitHub repo

In the repo (Settings → Secrets and variables → Actions):

| Type | Name | Value |
|------|------|-------|
| Variable | `AWS_DEPLOY_ROLE_ARN` | `arn:aws:iam::580909335056:role/safe-upload-github-deploy` |
| Variable | `AWS_REGION` | `eu-west-2` |
| Variable | `SAFE_UPLOAD_URL` | `https://03oljv1khe.execute-api.eu-west-2.amazonaws.com/dev` |
| Secret | `SAFE_UPLOAD_KEY` | The API key value (from `aws apigateway create-api-key` output) |

### 7. Branch protection

Settings → Branches → add a rule for `main`:

- Require a pull request before merging.
- Require status checks to pass before merging — select `ci-checks` (the job from `.github/workflows/ci.yml`).
- Disallow direct pushes (optional but recommended).

### 8. Verify

After Stage 5 lands, opening any PR should run `ci.yml`. Merging a PR to `main` should run `deploy.yml`, which will:

1. Re-run `ci-checks`.
2. Assume the `safe-upload-github-deploy` role via OIDC.
3. Run `make update-all` (uploads new lambda zips).
4. Run `make smoke` and `make security-smoke`.

## Permissions granted to the deploy role

Only what's needed:
- `lambda:UpdateFunctionCode` on `arn:aws:lambda:eu-west-2:580909335056:function:safe-upload-*` — to push new zips.
- `lambda:GetFunction` on the same — to read function config (used by some CLI flows).

The role cannot:
- Modify IAM, API Gateway, DynamoDB, S3 directly.
- Read CloudWatch logs.
- Invoke lambdas (which would let it spend account budget).

If we add CloudWatch log retention management, smoke-side DynamoDB checks, or future capabilities, they go in this policy file with a clear note.
