# API Gateway — `safe-upload-api`

REST API in front of the `safe-upload-create-file` Lambda. Lambda proxy integration on a `{proxy+}` resource so chi handles routing inside the function.

## Files

| File | Used by |
|------|---------|
| `rest-api.json` | `aws apigateway create-rest-api` |
| `proxy-resource.json` | `aws apigateway create-resource` |
| `proxy-method.json` | `aws apigateway put-method` |
| `lambda-integration.json` | `aws apigateway put-integration` |
| `lambda-permission.json` | `aws lambda add-permission` |
| `usage-plan.json` | `aws apigateway create-usage-plan` |

## Placeholders

Replace before applying:

- `<AWS_REGION>` — e.g. `eu-west-2`
- `<AWS_ACCOUNT_ID>` — 12-digit AWS account number
- `<REST_API_ID>` — output of `create-rest-api`
- `<ROOT_RESOURCE_ID>` — `rootResourceId` from the same output
- `<PROXY_RESOURCE_ID>` — output `id` from `create-resource`

## Apply order

```bash
# 1. Create the REST API container
aws apigateway create-rest-api \
  --cli-input-json file://infra/api-gateway/rest-api.json

# 2. Create the {proxy+} child of the root resource
aws apigateway create-resource \
  --cli-input-json file://infra/api-gateway/proxy-resource.json

# 3. Add ANY method on {proxy+} with API key required
aws apigateway put-method \
  --cli-input-json file://infra/api-gateway/proxy-method.json

# 4. Wire the Lambda proxy integration
aws apigateway put-integration \
  --cli-input-json file://infra/api-gateway/lambda-integration.json

# 5. Allow API Gateway to invoke the lambda
aws lambda add-permission \
  --cli-input-json file://infra/api-gateway/lambda-permission.json

# 6. Deploy to the dev stage (no JSON file — single command)
aws apigateway create-deployment \
  --rest-api-id <REST_API_ID> \
  --stage-name dev \
  --description "Initial deployment with {proxy+} -> safe-upload-create-file"

# 7. Create the usage plan bound to the dev stage
aws apigateway create-usage-plan \
  --cli-input-json file://infra/api-gateway/usage-plan.json

# 8. Create the API key (no JSON file — credential, name only)
aws apigateway create-api-key \
  --name safe-upload-mvp-key \
  --description "MVP API key for safe-upload-api dev stage" \
  --enabled

# 9. Associate the key with the usage plan
aws apigateway create-usage-plan-key \
  --usage-plan-id <USAGE_PLAN_ID> \
  --key-id <API_KEY_ID> \
  --key-type API_KEY
```

## Identity binding

The lambda reads `requestContext.identity.apiKeyId` and writes it to DynamoDB as `uploaded_by`. It does **not** read the `x-api-key` header — the header carries the secret value, which we don't want in the audit row. The `apiKeyId` is unique and stable per AWS account+region for the key's lifetime.

## Smoke test

```bash
export SAFE_UPLOAD_KEY=<key-value>
export SAFE_UPLOAD_URL=https://<rest-api-id>.execute-api.<region>.amazonaws.com/dev

# 1. Initiate multipart upload
curl -s -X POST "$SAFE_UPLOAD_URL/files" \
  -H "x-api-key: $SAFE_UPLOAD_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"smoke.txt","size":27}'

# 2. PUT bytes to each presigned URL (capture ETag from response headers)

# 3. Complete the upload
curl -s -X POST "$SAFE_UPLOAD_URL/files/<file-id>/complete-upload" \
  -H "x-api-key: $SAFE_UPLOAD_KEY" \
  -H "Content-Type: application/json" \
  -d '{"parts":[{"part_number":1,"etag":"<etag>"}]}'

# 4. Wait for GuardDuty scan, then download
curl -s "$SAFE_UPLOAD_URL/files/<file-id>" -H "x-api-key: $SAFE_UPLOAD_KEY"
```
