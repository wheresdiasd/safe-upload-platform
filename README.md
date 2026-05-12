# Rationale
As MR Fingers needs to upload documents on a system, where users from WEB/APPS/API's can upload files quickly without compromising the system security and file integrity. This project delivers a file upload system that is fast, secure and can be used across any areas of the platform and with a set of policies that protects the final user of having files exposed, even within the organisation. 

# Goals
* The system should be scalable and initially attend to:
* * Scale: How many files? (10M+ users, 50TB+ storage).
* * File Size: Mostly PDFs and Images (average 2MB, max 20MB).
* Increase the upload speed and create seamless CX.
* Mitigate security breaches
* Keep file metadata database


# Non-Goals
* IAM User groups
* Authentication
* VPC / Subnets configuration
* Lambda warm strategy / provisioned concurrency

# Proposed architecture
![Safe File Upload System - Page 1 High level design](https://github.com/user-attachments/assets/a8a21276-10a9-4d57-bc43-18ea78d265db)

![Safe File Upload System - Page 1 File creation](https://github.com/user-attachments/assets/9aceaa86-818b-4aa0-b9a6-7cb783634b41)
![Safe File Upload System - Page 1 File update](https://github.com/user-attachments/assets/661165c6-8f43-43b2-8414-170e8f0e78f7)
![Safe File Upload System - Page 2 Upload file](https://github.com/user-attachments/assets/a127c693-20e4-4eae-a28a-39e265ae8cbb)


# User/Client
The client component is the door to the upload platform, it allows final users to upload files via web interface or app, it can also be an API based as is possible to request pre-signed urls via API if you have a valid auth token.

# API Gateway
The API gateway will be the keeper between the application and the client, with exception for the presigned urls upload ( which is handled by AWS S3 ). It should have rate-limit rules and check authorization via Incognito ( which is not a part of this TDD ).

The gateway is a REST API with a single `{proxy+}` resource forwarding everything to one Lambda, which dispatches via [chi](https://github.com/go-chi/chi) internally. Three logical endpoints are exposed:

### `POST /files`
Initiates a multipart upload. Body `{ "name": "...", "size": 12345 }`. Returns the file ID, multipart upload ID, chunk size, and a list of presigned PUT URLs (one per 5 MB chunk).

### `POST /files/{id}/complete-upload`
Finalises the multipart upload after the client has PUT every chunk. Body `{ "parts": [{ "part_number": 1, "etag": "..." }] }`. Flips the DynamoDB row from `pending_upload` to `pending_scan`, after which GuardDuty Malware Protection takes over.

### `GET /files/{id}`
Returns a presigned download URL — but only when the file's status is `clean`. Files in `pending_scan`, `infected`, or `deleted` return 409.

### Auth & limits
- API key required at the gateway (`x-api-key` header).
- Identity is bound to `requestContext.identity.apiKeyId` — never the secret value — and recorded as `uploaded_by` in DynamoDB.
- Usage plan: 10 rps / burst 20 / 1000 requests per day.
- 20 MB max file size; designed to scale for 10M+ users and 50TB+ storage.

Full setup as JSON-as-data in [`infra/api-gateway/`](./infra/api-gateway/).

# S3 Bucket
This is our go to as market default and high scalability, speed ( presigned links support streamlined chunk uploads ), support, good development experience and integrates with other AWS products like the event bridge. It needs to have a set of policies set in order to achieve system security: a bucket policy denies `s3:GetObject` unless GuardDuty has tagged the object with `NO_THREATS_FOUND`, and files which are compromised are deleted outright by a remediation Lambda triggered via EventBridge.
## Trade-offs 
Decided to delete infected files outright instead of keeping a second quarantine bucket. Once GuardDuty flags `THREATS_FOUND`, the remediation Lambda performs an `s3:DeleteObject` (with `VersionId` — the bucket has versioning) and updates the DynamoDB row to `status=deleted` with a 30-day TTL. The DynamoDB row is the audit trail; the file bytes add no value once we have the metadata. A second bucket would only become necessary if forensic copies of malicious uploads were a hard requirement.

# Amazon EventBridge
# Amazon GuarDuty
# AWS Lambda ([setup](./lambda/README.md))
## Trade-offs 
### Chi Router vs Terraform + IaC
As this project is for personal usage and ramping up on lambda architecture integrated with go language, we followed a simple approach by using chi Router which creates a monolithic lambda. This is not a recommended approach on real businesses, but here we choose fast shipping / learning process / code structure organisation over IaC w Terraform Learning curve / Granularity of permissioning. However we should also bear in mind other trade-offs such as cold starts / memory usage per endpoint ( which are now coupled ).
![Safe File Upload System - Page 3 Lambda architecture - CHI Router vs IaC](https://github.com/user-attachments/assets/fc820399-d9bf-4475-9ac4-7e8186ce1d01)



# Dynamo DB

# Testing
End-to-end coverage runs against the deployed `dev` stage as black-box scripts — no AWS credentials needed beyond the public API key.

| Script | What it proves |
|--------|----------------|
| [`tests/smoke.sh`](./tests/smoke.sh) | Happy path: POST /files → PUT bytes to presigned URL → complete-upload → GuardDuty scan completes → status `clean` → presigned download → bytes round-trip |
| [`tests/security-smoke.sh`](./tests/security-smoke.sh) | Security path: EICAR signature uploaded → GuardDuty flags it → remediation Lambda deletes the object → GET /files/{id} stays 409 (never returns 200). EICAR pattern generated inline; no on-disk fixture required |

Run via `make smoke` and `make security-smoke` from repo root after exporting `SAFE_UPLOAD_URL` and `SAFE_UPLOAD_KEY`. See [`tests/README.md`](./tests/README.md) for env-var setup, tunable timeouts, and troubleshooting notes.

# CI/CD

Two-tier defence-in-depth gate, with the same checks running locally before commit and in CI on push.

## The checks (`make ci-checks`)

`ci-checks` is a chain of four targets, ordered cheapest-first so failures surface fast.

### 1. `gofmt -l lambda/`
**What:** lists Go source files whose formatting doesn't match the canonical style. Empty output = pass; any file listed = fail.
**Why:** Go has a single, opinionated formatter. Treating "is the code formatted?" as a check eliminates an entire category of code-review bikeshedding (tabs vs spaces, brace placement, import ordering) and makes diffs cleaner — every formatting commit is intentional.
**Cost:** sub-second.

### 2. `go vet ./...`
**What:** static analysis catching suspicious constructs the compiler accepts but are usually bugs — printf format-string mismatches, unreachable code, struct-tag typos, locks copied by value, etc.
**Why:** it's a bug detector, not a linter. The Go standard library team treats `go vet` failures as compile errors. Cheap, catches real bugs.
**Limits:** doesn't replace a real linter (`golangci-lint`); we may layer those in later if needed.

### 3. `make build` → `lambda/Makefile build-all`
**What:** cross-compiles both Lambda binaries (`GOOS=linux GOARCH=arm64`) and zips them ready for `update-function-code`.
**Why:** "compiles on my machine" ≠ "builds for the Lambda runtime." Same toolchain CI uses, same target architecture as production. Missing imports, broken module references, type errors all surface here.
**Side-effect:** the zips it produces are exactly what `deploy.yml` later uploads — no surprise rebuilds at deploy time.

### 4. `gitleaks detect --source . --no-banner --redact`
**What:** scans the working tree **and full git history** for patterns matching known secret formats (AWS access keys, GitHub tokens, Stripe keys, generic high-entropy strings, etc.). Default ruleset has ~150 detectors.
**Why:** once a secret is committed it's effectively published — `git log` retains it, public repos are mirrored and indexed within minutes. Local dev hygiene cannot be the only line of defence.
**Real catch:** during setup gitleaks flagged `lambda/output.json` (committed in `5b0ff1f`) containing two `ASIA...`-prefixed STS temporary tokens. They had a 15-minute expiry from March, so functionally dead — but gitleaks correctly flagged them. We deleted the file, gitignored it, and wrote a [`.gitleaks.toml`](./.gitleaks.toml) allowlist for that path. History wasn't rewritten because the credentials were already expired.
**`--redact`:** prints "REDACTED" in place of the matched value so logs don't leak the secret further.

What's deliberately deferred:
- **Unit tests** (`go test ./...`) — Lambdas hold AWS clients in package globals and need an interface refactor before they're testable. Tracked as Tier 2.
- **Integration tests** — covered by the smoke scripts post-deploy (Tier 3).
- **Linters** (`golangci-lint`, `gosec`) — useful but not pulling their weight on a single-Lambda project yet.

## Where the checks run

- **Local pre-commit hook** ([`scripts/pre-commit.sh`](./scripts/pre-commit.sh), installed via `make install-hooks`) runs `make ci-checks` on every `git commit`. Bypassable with `--no-verify`; CI is the backstop.
- **PR gate** ([`.github/workflows/ci.yml`](./.github/workflows/ci.yml)) runs the same checks on every pull request to `main`. Required status check; merge blocked until green.
- **Deploy** ([`.github/workflows/deploy.yml`](./.github/workflows/deploy.yml)) re-runs `ci-checks` defensively, then deploys, then re-runs the smoke scripts.

## Auto-deploy on merge — OIDC

The deploy workflow fires on push to `main` (i.e. PR merge) and updates both Lambda zips. To call AWS it needs credentials. We don't store any.

### The problem we're avoiding
The "obvious" path is to create an IAM user, generate access keys (`AKIA...` + secret), store them as GitHub repo secrets, and let the workflow export them as env vars. This works, and it's how a lot of AWS credential leaks have happened. Problems:

- **Long-lived**: keys are valid until manually rotated. Leaks (compromised laptop, malicious workflow dep, ex-contributor) stay valid.
- **Out of band**: GitHub stores them; AWS doesn't know where they live or who's using them.
- **Hard to scope and rotate**: usually end up over-privileged because per-workflow IAM users are operationally painful.

### What OIDC gives us instead
**OpenID Connect** is a thin auth layer on OAuth 2.0. The model: an **issuer** signs short-lived JWTs proving "this caller is X." A **relying party** verifies the signature against the issuer's public keys and trusts the claims.

In our case:
- **Issuer**: GitHub's OIDC service at `https://token.actions.githubusercontent.com`. Mints a JWT for every workflow run, automatically.
- **Relying party**: AWS IAM. We told it (by creating an Identity Provider resource) that this issuer is trusted; AWS knows where to fetch its public keys from `/.well-known/openid-configuration`.
- **Action**: `sts:AssumeRoleWithWebIdentity` — exchanges a verified JWT for short-lived AWS credentials.

### The runtime flow on every deploy

1. **`deploy.yml` starts** because someone pushed to `main`.
2. **The runner asks GitHub's OIDC service** for a token. GitHub mints a signed JWT containing claims like:
   - `iss`: `https://token.actions.githubusercontent.com`
   - `aud`: `sts.amazonaws.com` (we asked for this audience via `permissions: id-token: write`)
   - `sub`: `repo:wheresdiasd/safe-upload-platform:ref:refs/heads/main`
   - `repository`, `ref`, `actor`, `run_id`, `sha`, etc.
3. **`aws-actions/configure-aws-credentials@v4`** receives the token and calls `sts:AssumeRoleWithWebIdentity` against AWS, passing the JWT and the role ARN.
4. **AWS STS verifies the JWT signature** against GitHub's public keys, then evaluates the role's **trust policy**:
   ```json
   "Condition": {
     "StringEquals": {
       "token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
       "token.actions.githubusercontent.com:sub": "repo:wheresdiasd/safe-upload-platform:ref:refs/heads/main"
     }
   }
   ```
   Only allow this assume call if the audience matches AND the subject is exactly that repo + branch.
5. **STS issues short-lived credentials** — typically valid for 1 hour. The action exports them as env vars (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`, all `***`-redacted in logs). When the job ends, they expire on their own.
6. **The rest of the workflow** uses those credentials for `aws lambda update-function-code` etc.

### Why the trust scope matters

The `sub` claim is the security boundary:

- **Forked-repo attack.** Someone forks the repo and tries to assume the role from a malicious workflow. Their `sub` is `repo:attacker/safe-upload-platform:...` — doesn't match. Denied.
- **Wrong-branch attack.** A PR from a feature branch in our own repo runs malicious code. Their `sub` is `...:ref:refs/heads/feature-x`, doesn't match `main`. Denied. (`deploy.yml` only fires on push to main anyway — defence in depth.)
- **Different repo.** Some unrelated repo of yours tries to assume the role. Different `repository` claim, different `sub`. Denied.

### What the deploy role can actually do

Once assumed, the role's **permission policy** ([`infra/iam/github-actions-deploy-policy.json`](./infra/iam/github-actions-deploy-policy.json)) is intentionally tiny:

```json
{
  "Action": [
    "lambda:UpdateFunctionCode",
    "lambda:GetFunction",
    "lambda:GetFunctionConfiguration"
  ],
  "Resource": "arn:aws:lambda:<AWS_REGION>:<AWS_ACCOUNT_ID>:function:safe-upload-*"
}
```

Even with valid temporary credentials, the role cannot:
- Touch IAM, API Gateway, DynamoDB, or S3.
- Read CloudWatch logs.
- Invoke Lambdas (so a compromised workflow can't burn the AWS budget by spamming invocations).
- Create new functions, only update existing `safe-upload-*` ones.

Principle of least privilege made concrete.

### Where everything lives

- **AWS side**: the OIDC Identity Provider resource + the `safe-upload-github-deploy` role. Created once via console; templates in [`infra/iam/github-actions-deploy-trust-policy.json`](./infra/iam/github-actions-deploy-trust-policy.json) and [`github-actions-deploy-policy.json`](./infra/iam/github-actions-deploy-policy.json).
- **GitHub side**: one repo variable (`AWS_DEPLOY_ROLE_ARN`) telling the workflow which role to assume. **No long-lived secret.**
- **Walkthrough**: [`infra/iam/github-actions-oidc-setup.md`](./infra/iam/github-actions-oidc-setup.md) documents the entire one-time setup.
