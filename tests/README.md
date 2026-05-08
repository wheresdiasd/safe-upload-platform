# Tests

Two black-box end-to-end test scripts that exercise the deployed `dev` stage of the platform. They run against API Gateway only — no AWS CLI required, no AWS credentials needed (just the API key).

| Script | What it proves |
|--------|----------------|
| `smoke.sh` | Happy path: POST /files → PUT bytes → complete-upload → GuardDuty scan → status `clean` → presigned download → bytes round-trip |
| `security-smoke.sh` | Security path: EICAR signature uploaded → GuardDuty flags it → `update-file` deletes the object → GET /files/{id} stays 409 (never returns 200). The EICAR pattern is generated inline by the script — no on-disk fixture required. |

## Required env vars

```bash
export SAFE_UPLOAD_URL=https://<rest-api-id>.execute-api.<region>.amazonaws.com/<stage>
export SAFE_UPLOAD_KEY=<api-key-value>
```

Keep `SAFE_UPLOAD_KEY` out of git history. Two acceptable patterns:

- Set them in `~/.zshrc.local` (or equivalent) and source it from your main shell rc.
- Use a `.env.local` file (already covered by `.gitignore`) and `set -a; source .env.local; set +a` before running.

## Running

From repo root:

```bash
make smoke           # happy path
make security-smoke  # EICAR
make test            # both
```

Or directly:

```bash
bash tests/smoke.sh
bash tests/security-smoke.sh
```

## Tunable timeouts

| Env var | Default | Script |
|---|---|---|
| `SMOKE_TIMEOUT_SECONDS` | 90 | `smoke.sh` — how long to wait for status to transition to `clean` |
| `SECURITY_SMOKE_WAIT_SECONDS` | 45 | `security-smoke.sh` — how long to keep polling, asserting GET never returns 200 |

## Expected output

`smoke.sh` ends with:

```
==> Stage 5: GET presigned download and diff bytes
MATCH
Smoke test PASSED (fileID=...)
```

`security-smoke.sh` ends with:

```
PASSED: GET /files/<id> returned 409 consistently across 45s window
        (fileID=...; black-box assertion — see header comment for caveat)
```

## Troubleshooting

- **Smoke times out at Stage 4**: GuardDuty scanning is slower than usual. Bump `SMOKE_TIMEOUT_SECONDS` to 180 and re-run. If consistently slow, check the GuardDuty Malware Protection plan in the AWS console.
- **`HTTP 403 Forbidden`**: the API key is missing, wrong, or unbound from the usage plan. Verify `SAFE_UPLOAD_KEY` matches the value of the key in `apigateway.create-api-key`.
- **`HTTP 429`**: throttle or quota exceeded. The current usage plan is 10 rps / burst 20 / 1000 requests/day.
- **PUT fails with `403 SignatureDoesNotMatch`**: the presigned URL expired (15 minute window) or you modified the bytes between getting the URL and uploading.
- **EICAR test ends with `200`**: regression in the security pipeline. Either GuardDuty isn't tagging, EventBridge isn't routing to `update-file`, or `update-file` isn't deleting/updating. Check the `update-file` lambda logs in CloudWatch.

## Black-box scope

`security-smoke.sh` is intentionally black-box — it only uses the public API. It cannot fully distinguish "scan still in progress" from "scan completed and remediated" because both produce 409 from `GET /files/{id}`. For full confidence that the DynamoDB row reached `status=deleted`, an out-of-band check is needed:

```bash
aws dynamodb get-item \
  --table-name safe-upload-files \
  --key '{"id":{"S":"<file-id-from-test-output>"}}' \
  --region eu-west-2 \
  --query 'Item.status.S' --output text
```

Adding this to the script would require AWS CLI and DynamoDB read permissions in CI. Deferred to keep the deploy role's permission scope minimal.
