# AWS Budgets — `safe-upload-monthly`

Monthly $15 spend ceiling with three email tripwires. Budgets are a free AWS service (up to 2 free per account; $0.02/budget/day beyond that).

## Files

| File | Used by |
|------|---------|
| `budget-monthly.json` | `aws budgets create-budget --budget file://...` |
| `notifications.json` | `aws budgets create-budget --notifications-with-subscribers file://...` |

## Placeholders

Replace before applying:

- `<NOTIFICATION_EMAIL>` — email that receives the budget alerts (currently `diegodiasengineer@gmail.com`)

## Apply

```bash
ACCOUNT=580909335056
EMAIL=diegodiasengineer@gmail.com

sed "s/<NOTIFICATION_EMAIL>/$EMAIL/g" infra/budgets/notifications.json > /tmp/budget-notifications.json

aws budgets create-budget \
  --account-id "$ACCOUNT" \
  --budget file://infra/budgets/budget-monthly.json \
  --notifications-with-subscribers file:///tmp/budget-notifications.json
```

This single call creates the budget and all three notification thresholds in one shot. Each subscriber is added automatically.

## Threshold rationale

| Threshold | Type | Trigger | Why |
|---|---|---|---|
| 50% ($7.50) | ACTUAL | Real spend crosses half-budget mid-month | Early "you're trending hot" signal |
| 80% ($12.00) | FORECASTED | AWS projects month-end spend will exceed $12 | Predictive — fires before the money actually lands |
| 100% ($15.00) | ACTUAL | Hard overrun | The line in the sand |

## Verify

```bash
aws budgets describe-budget \
  --account-id 580909335056 \
  --budget-name safe-upload-monthly

aws budgets describe-notifications-for-budget \
  --account-id 580909335056 \
  --budget-name safe-upload-monthly
```

Or in the console: **Billing → Budgets → `safe-upload-monthly`**.

## Email subscription

Unlike SNS, AWS Budgets does NOT send a confirmation email — once you create the subscriber, notifications will be delivered to that address with no opt-in step. Be sure the address is correct; correcting it later means deleting and recreating the subscriber.

## Deferred

- **Per-service budget filters** (e.g. only S3, only Lambda) — not needed for a learning project with predictable cost shape.
- **SNS-routed alerts** — Budgets supports SNS as a subscription type. Skipped because the email path is simpler and one less integration. Revisit if alarms need fan-out to multiple channels.
- **Cost anomaly detection** — separate AWS service; out of scope for this stage.
