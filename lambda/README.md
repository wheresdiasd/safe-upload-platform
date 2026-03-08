# Lambda Functions

## Prerequisites

- [Go 1.21+](https://go.dev/dl/)
- [AWS CLI v2](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html) configured with `aws configure`

## Environment Variables

Add these to your shell profile (`~/.zshrc` or `~/.bashrc`):

```bash
export AWS_BUCKET_NAME=<your-s3-bucket-name>
export AWS_TABLE_NAME=<your-dynamodb-table-name>
export AWS_ROLE_ARN=<your-lambda-execution-role-arn>
export AWS_REGION=<your-aws-region>
```

Then reload:

```bash
source ~/.zshrc
```

## Build and Deploy

```bash
# Build both functions
make build-all

# Deploy both functions (first time)
make deploy-all

# Deploy individually
make deploy-create-file
make deploy-update-file

# Clean build artifacts
make clean
```

## Functions

| Function | Trigger | Purpose |
|----------|---------|---------|
| `create-file` | API Gateway | Generates multipart presigned URLs for file upload |
| `update-file` | EventBridge | Handles GuardDuty scan results — updates file status, deletes infected files |
