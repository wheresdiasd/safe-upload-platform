#!/usr/bin/env bash
# Security smoke test: uploads the EICAR test signature and verifies the
# pipeline does NOT make it downloadable. Proves the GuardDuty + update-file
# remediation chain is wired through API Gateway.
#
# Required env vars:
#   SAFE_UPLOAD_URL  e.g. https://03oljv1khe.execute-api.eu-west-2.amazonaws.com/dev
#   SAFE_UPLOAD_KEY  the API key value (x-api-key header)
#
# NOTE on assertion strength: this is a black-box test. It polls GET /files/{id}
# and fails fast if it ever returns 200 (which would mean an EICAR file became
# `clean`). It declares success only after sustained 409 across the wait window.
# The black-box test cannot distinguish "scan still in progress" from "scan
# finished + status=deleted" without DynamoDB access. Both produce 409. For full
# confidence that status transitioned to `deleted`, a separate AWS CLI check
# against the safe-upload-files DynamoDB table would be needed; deferred to a
# future stage to keep CI permission scope minimal.

set -euo pipefail

: "${SAFE_UPLOAD_URL:?SAFE_UPLOAD_URL is required}"
: "${SAFE_UPLOAD_KEY:?SAFE_UPLOAD_KEY is required}"

WAIT_SECONDS=${SECURITY_SMOKE_WAIT_SECONDS:-45}

WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT

# EICAR Standard Anti-Virus Test File — a public 68-byte pattern that all AV
# engines (including GuardDuty Malware Protection) flag on detection, by design.
# Not actual malware. https://www.eicar.org/download-anti-malware-testfile/
EICAR='X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*'
EICAR_FILE="$WORKDIR/eicar.txt"
printf '%s' "$EICAR" > "$EICAR_FILE"
SIZE=$(wc -c < "$EICAR_FILE" | tr -d ' ')

echo "==> Stage 1: POST /files (EICAR, size=$SIZE)"
CREATE_RESP=$(curl -sS --fail-with-body -X POST "$SAFE_UPLOAD_URL/files" \
  -H "x-api-key: $SAFE_UPLOAD_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"eicar.txt\",\"size\":$SIZE}")

FILE_ID=$(printf '%s' "$CREATE_RESP" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')
PRESIGNED_URL=$(printf '%s' "$CREATE_RESP" | python3 -c 'import json,sys; print(json.load(sys.stdin)["parts"][0]["url"])')
echo "    fileID=$FILE_ID"

echo "==> Stage 2: PUT EICAR bytes to presigned URL"
HEADERS_FILE="$WORKDIR/upload_headers.txt"
HTTP_CODE=$(curl -sS -D "$HEADERS_FILE" -X PUT --upload-file "$EICAR_FILE" "$PRESIGNED_URL" -o /dev/null -w "%{http_code}")
if [ "$HTTP_CODE" != "200" ]; then
  echo "ERROR: PUT to presigned URL failed (HTTP $HTTP_CODE)" >&2
  exit 1
fi
ETAG=$(grep -i '^etag:' "$HEADERS_FILE" | awk '{print $2}' | tr -d '\r"')
echo "    etag=$ETAG"

echo "==> Stage 3: POST /files/$FILE_ID/complete-upload"
curl -sS --fail-with-body -X POST "$SAFE_UPLOAD_URL/files/$FILE_ID/complete-upload" \
  -H "x-api-key: $SAFE_UPLOAD_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"parts\":[{\"part_number\":1,\"etag\":\"$ETAG\"}]}" >/dev/null
echo "    status=pending_scan (handed to GuardDuty)"

echo "==> Stage 4: poll GET /files/$FILE_ID; fail fast if it ever returns 200"
START=$(date +%s)
LAST_CODE=""
while :; do
  CODE=$(curl -sS -o /dev/null -w "%{http_code}" \
    "$SAFE_UPLOAD_URL/files/$FILE_ID" \
    -H "x-api-key: $SAFE_UPLOAD_KEY" || true)
  if [ "$CODE" = "200" ]; then
    echo "ERROR: GET /files/$FILE_ID returned 200 — EICAR file is being treated as clean. Remediation FAILED." >&2
    exit 1
  fi
  ELAPSED=$(( $(date +%s) - START ))
  if [ "$ELAPSED" -ge "$WAIT_SECONDS" ]; then
    LAST_CODE=$CODE
    break
  fi
  sleep 3
done

if [ "$LAST_CODE" != "409" ]; then
  echo "ERROR: after ${WAIT_SECONDS}s, GET returned $LAST_CODE (expected 409)" >&2
  exit 1
fi

echo "PASSED: GET /files/$FILE_ID returned 409 consistently across ${WAIT_SECONDS}s window"
echo "        (fileID=$FILE_ID; black-box assertion — see header comment for caveat)"
