# SMS failures

Alert `tz-auth-sms-failures` in `infra/grafana/alerts/auth.yaml` (05 §5, owner W3-07). Severity: **critical**.

## What it means

More than 5 % of OTP SMS sends failed over 10 minutes, for 10 minutes. People cannot sign in.
The OTP SMS is sent by identity directly (not notify), and a failure is the log line
`identity: OTP SMS failed`; the rule divides those lines by the `otp_requests` made in the window.

## First three checks

1. [12 · Security and abuse](https://grafana.todayz.localhost/d/todayz-12): OTP requests and failures.
2. Loki: `{service_name="todayz-api"} |= "identity: OTP SMS failed"`; the error names the provider's answer (never the phone or the code).
3. The SMS provider's status page and the account balance (the founder's console).

## Likely causes

- The provider is down or throttling.
- The account ran out of credit or the sender id / DLT template was rejected.
- A key rotated without updating the environment (`make env-check`).

## Fix

- Provider or account: the founder fixes it in the provider console. A key: the lead updates the environment.
- Do not raise OTP limits to compensate.

## Who to tell

- The **lead** and the founder at once; **W1-01** (identity).
