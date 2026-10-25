# OTP abuse

Alerts `tz-auth-otp-spike`, `tz-auth-otp-one-address` in `infra/grafana/alerts/auth.yaml` (05 §5, owner W3-07). Severity: **warning**.

## What it means

OTP sends look like abuse (SMS pumping or brute force). Spike: at least 20 sends in 10 minutes,
more than 5 times the 7-day average per 10 minutes. One address: one hashed client address asked
for more than 50 OTPs in an hour. The 05 §5 rule is per /24; `otp_requests.ip_hash` is per address
(TODO(lead): a prefix hash). The alert carries only the first 12 hex characters of the hash.

## First three checks

1. [12 · Security and abuse](https://grafana.todayz.localhost/d/todayz-12): OTP sends over time, by app and purpose; top address keys.
2. `otp_requests` (read-only, as `todayz_owner` in a read-only transaction): `SELECT left(encode(ip_hash,'hex'),12), app, purpose, count(*) FROM otp_requests WHERE created_at > now() - interval '1 hour' GROUP BY 1,2,3 ORDER BY 4 DESC LIMIT 20`. Never select `phone_enc`.
3. WAF / rate-limit logs for the same window (Cloudflare, cloud only).

## Likely causes

- A bot sending to premium-rate numbers (pumping).
- A marketing push driving real sign-ins (a spike with many addresses and normal success).

## Fix

- Real sign-ins: nothing to do. Abuse: the per-phone and per-address limits in `platform_settings` hold; add a WAF rule for the source (the lead, cloud only).

## Who to tell

- **W1-01** (identity) and the lead.
