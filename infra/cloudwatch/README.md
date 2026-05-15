# CloudWatch — `safe-upload-platform` observability

Dashboard + three alarms covering the API Gateway, both Lambdas, and DynamoDB. Alarm actions publish to the `safe-upload-alarms` SNS topic; subscribers receive notifications on transitions to `ALARM` and back to `OK`.

## Files

| File | Used by |
|------|---------|
| `dashboard.json` | `aws cloudwatch put-dashboard --dashboard-body file://...` |
| `alarm-create-file-errors.json` | `aws cloudwatch put-metric-alarm --cli-input-json file://...` |
| `alarm-update-file-errors.json` | `aws cloudwatch put-metric-alarm --cli-input-json file://...` |
| `alarm-api-5xx.json` | `aws cloudwatch put-metric-alarm --cli-input-json file://...` |

## Placeholders

Replace before applying:

- `<AWS_REGION>` — e.g. `eu-west-2`
- `<AWS_ACCOUNT_ID>` — 12-digit AWS account number

## Apply order

```bash
REGION=eu-west-2
ACCOUNT=580909335056

# 1. SNS topic that all alarms publish to. Subscribe an email; AWS sends a
#    confirmation link that must be clicked before notifications are delivered.
aws sns create-topic --name safe-upload-alarms --region "$REGION"
aws sns subscribe \
  --topic-arn "arn:aws:sns:$REGION:$ACCOUNT:safe-upload-alarms" \
  --protocol email \
  --notification-endpoint <your-email-address> \
  --region "$REGION"

# 2. Log retention (default is Never Expire = unbounded cost)
aws logs put-retention-policy \
  --log-group-name /aws/lambda/safe-upload-create-file \
  --retention-in-days 14 --region "$REGION"
aws logs put-retention-policy \
  --log-group-name /aws/lambda/safe-upload-update-file \
  --retention-in-days 14 --region "$REGION"

# 3. Alarms (substitute placeholders in each JSON first)
for f in infra/cloudwatch/alarm-*.json; do
  sed -e "s/<AWS_REGION>/$REGION/g" -e "s/<AWS_ACCOUNT_ID>/$ACCOUNT/g" "$f" > /tmp/alarm.json
  aws cloudwatch put-metric-alarm --cli-input-json file:///tmp/alarm.json --region "$REGION"
done

# 4. Dashboard
aws cloudwatch put-dashboard \
  --dashboard-name safe-upload-platform \
  --dashboard-body file://infra/cloudwatch/dashboard.json \
  --region "$REGION"
```

## Alarm design

All three alarms share the same shape:

- **Threshold**: `>= 1 over a single 5-minute period`. Sensitive — appropriate for the low-traffic MVP. Easy to dial up once we know the baseline.
- **`TreatMissingData: notBreaching`** — no data = healthy, not alarm. Without this the alarm would sit in `INSUFFICIENT_DATA` indefinitely when traffic is sparse.
- **`AlarmActions` and `OKActions` both point at the SNS topic** — we want to know when something goes wrong *and* when it recovers.

## Smoke testing the alarm path

The cleanest non-invasive way to exercise the SNS notification chain (once email is confirmed) is to push an alarm into `ALARM` and back manually:

```bash
aws cloudwatch set-alarm-state \
  --alarm-name safe-upload-create-file-errors \
  --state-value ALARM \
  --state-reason "Manual smoke test" \
  --region eu-west-2
# wait for email, then:
aws cloudwatch set-alarm-state \
  --alarm-name safe-upload-create-file-errors \
  --state-value OK \
  --state-reason "Manual smoke test reset" \
  --region eu-west-2
```

Requires `cloudwatch:SetAlarmState` (not in the current `safe-upload-cloudwatch-access` policy — add if you want to run this).

## Dashboard URL

[safe-upload-platform](https://eu-west-2.console.aws.amazon.com/cloudwatch/home?region=eu-west-2#dashboards:name=safe-upload-platform) (region-specific, console-only).
