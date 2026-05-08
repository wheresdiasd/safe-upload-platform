#!/usr/bin/env bash
# Happy-path smoke test for safe-upload-platform.
# Exercises the API Gateway -> Lambda -> S3 -> GuardDuty -> DynamoDB pipeline end-to-end.
#
# Required env vars:
#   SAFE_UPLOAD_URL  e.g. https://03oljv1khe.execute-api.eu-west-2.amazonaws.com/dev
#   SAFE_UPLOAD_KEY  the API key value (x-api-key header)
#
# Exits 0 on success, non-zero on any failure with a diagnostic message.

set -euo pipefail

: "${SAFE_UPLOAD_URL:?SAFE_UPLOAD_URL is required}"
: "${SAFE_UPLOAD_KEY:?SAFE_UPLOAD_KEY is required}"

SMOKE_TIMEOUT_SECONDS=${SMOKE_TIMEOUT_SECONDS:-90}
WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT

PAYLOAD="$WORKDIR/payload.txt"
DOWNLOADED="$WORKDIR/downloaded.txt"
echo "smoke test $(date -u +%FT%TZ) $$" > "$PAYLOAD"
SIZE=$(wc -c < "$PAYLOAD" | tr -d ' ')

echo "==> Stage 1: POST /files (size=$SIZE)"
CREATE_RESP=$(curl -sS --fail-with-body -X POST "$SAFE_UPLOAD_URL/files" \
  -H "x-api-key: $SAFE_UPLOAD_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"smoke.txt\",\"size\":$SIZE}")

FILE_ID=$(printf '%s' "$CREATE_RESP" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')
PRESIGNED_URL=$(printf '%s' "$CREATE_RESP" | python3 -c 'import json,sys; print(json.load(sys.stdin)["parts"][0]["url"])')
echo "    fileID=$FILE_ID"

echo "==> Stage 2: PUT bytes to presigned URL"
HEADERS_FILE="$WORKDIR/upload_headers.txt"
HTTP_CODE=$(curl -sS -D "$HEADERS_FILE" -X PUT --upload-file "$PAYLOAD" "$PRESIGNED_URL" -o /dev/null -w "%{http_code}")
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
echo "    status=pending_scan"

echo "==> Stage 4: poll GET /files/$FILE_ID until clean (timeout ${SMOKE_TIMEOUT_SECONDS}s)"
START=$(date +%s)
DOWNLOAD_URL=""
while :; do
  RESP=$(curl -sS -o "$WORKDIR/get_resp.json" -w "%{http_code}" \
    "$SAFE_UPLOAD_URL/files/$FILE_ID" \
    -H "x-api-key: $SAFE_UPLOAD_KEY" || true)
  if [ "$RESP" = "200" ]; then
    DOWNLOAD_URL=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["url"])' "$WORKDIR/get_resp.json")
    break
  fi
  ELAPSED=$(( $(date +%s) - START ))
  if [ "$ELAPSED" -ge "$SMOKE_TIMEOUT_SECONDS" ]; then
    echo "ERROR: timed out after ${SMOKE_TIMEOUT_SECONDS}s waiting for status=clean (last HTTP $RESP)" >&2
    cat "$WORKDIR/get_resp.json" >&2 || true
    exit 1
  fi
  sleep 3
done
echo "    clean after ${ELAPSED}s"

echo "==> Stage 5: GET presigned download and diff bytes"
curl -sS --fail-with-body "$DOWNLOAD_URL" -o "$DOWNLOADED"
if ! diff -q "$PAYLOAD" "$DOWNLOADED" >/dev/null; then
  echo "ERROR: downloaded bytes do not match uploaded payload" >&2
  diff "$PAYLOAD" "$DOWNLOADED" >&2 || true
  exit 1
fi

echo "MATCH"
echo "Smoke test PASSED (fileID=$FILE_ID)"
