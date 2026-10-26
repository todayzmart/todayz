# SMS failures

Alert `tz-auth-sms-failures` in `infra/grafana/alerts/auth.yaml` (05 §5, owner W3-07). Severity: **critical**.

## What it means

More than 5 % of OTP SMS sends failed over 10 minutes, for 10 minutes. People cannot sign in.
The OTP SMS is sent by identity directly (not notify), and a failure is the log line
`identity: OTP SMS failed`; the rule divides those lines by the `otp_requests` made in the window.

## First five minutes

1. Loki: `{service_name="todayz-api"} |= "identity: OTP SMS failed"`: what does the provider answer?
2. Check the provider's status page and the account balance (the founder's console).
3. Open an [incident note](../ops/incident-template.md) and tell the lead and the founder (the provider and DLT are theirs).

## Diagnosis

1. [12 · Security and abuse](https://grafana.todayz.localhost/d/todayz-12): OTP requests and failures.
2. Loki: `{service_name="todayz-api"} |= "identity: OTP SMS failed"`; the error names the provider's answer (never the phone or the code).
3. The SMS provider's status page and the account balance (the founder's console).

### Likely causes

- The provider is down or throttling.
- The account ran out of credit or the sender id / DLT template was rejected.
- A key rotated without updating the environment (`make env-check`).

## Mitigation

- Provider or account: the founder fixes it in the provider console. A key: the lead updates the environment.
- Do not raise OTP limits to compensate.

## Escalation

**Level:** Critical: SEV1 if most sends fail (nobody new can sign in), else SEV2 ([Severity levels](../ops/severity.md)).

- The **lead** and the founder at once; **W1-01** (identity).

## Follow-up

- If it was balance or DLT template expiry, a reminder or alert before it runs out.
- Record the provider's answer and how long it lasted.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
