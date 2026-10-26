# Client errors

Alerts `tz-client-errors`, `tz-client-release-errors` in `infra/grafana/alerts/client.yaml` (05 §5, owner W3-07). Severity: **warning**.

## What it means

JS errors (chunk-load failures included) are above 2 % of page views for an app over 30 minutes,
or one app version's rate is twice its app's rate over the day before. RUM has no session counter,
so "error sessions" is read as errors per page view, with a floor of 100 page views
(TODO(lead): a session id in the RUM beacon).

## First five minutes

1. Open [05 · Customer RUM](https://grafana.todayz.localhost/d/todayz-05): which app and version?
2. If one new version carries the errors, roll that Pages project back ([Release checklist → Rollback](../ops/release-checklist.md#rollback)).
3. Chunk-load errors right after a deploy fade as tabs reload; watch for 15 minutes before acting.

## Diagnosis

1. [05 · Customer RUM](https://grafana.todayz.localhost/d/todayz-05): errors by app, version and route; LCP by version.
2. Sentry (cloud): the top issue for that release.
3. Was a release just deployed (Cloudflare Pages deploy time)?

### Likely causes

- A new release with a bug.
- Chunk-load errors after a deploy (old tabs asking for removed files).
- A third-party script or browser update.

## Mitigation

- A bad release: roll back the Pages deployment (the lead).
- Chunk errors: they fade as tabs reload; the service worker should prompt for the update.

## Escalation

**Level:** Warning: SEV3. SEV2 if the customer or business app is unusable for many people ([Severity levels](../ops/severity.md)).

- The owner of the app (customer, business, admin) and the lead.

## Follow-up

- A test or an e2e check for the broken screen.
- If chunk errors were the cause, check the service worker's update prompt.
- SEV1 or SEV2: finish the [incident note](../ops/incident-template.md) and its review within two working days.
