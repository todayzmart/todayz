# W2-05 Realtime and notifications: QA report

This covers server-sent events for the store, customer and admin apps, and the notification core: inbox, SMS/WhatsApp through MSG91, and the "new order not seen" escalation. Branch `worktree-agent-a8795ccc34a8d303d`, rebased onto `main` 1fb3ff4 on 2026-10-23. Everything ran locally. Providers were reached only through httptest servers, and nothing touched a cloud account.

## What was built

- **Realtime outbox (`internal/platform/realtime`):**
  - `Publish` writes `{type,id,version}` to `outbox_events` in the caller's transaction and calls `pg_notify`. A rolled-back transaction sends nothing.
  - Rows are kept 24 h. A River cleanup job removes them and raises a watermark.
  - Channels are `store:{id}`, `user:{id}` and `admin`. An event registry says which types each channel kind may carry.
- **SSE hub:**
  - One LISTEN connection per instance, with bounded per-stream buffers. A slow consumer is dropped and told to reconnect.
  - `Last-Event-ID` replays from an xid8 cursor. It sends `reset` when the cursor is too old, from the future, or over the replay limit. An event already replayed is not sent again live.
  - A heartbeat every 25 s carries an id-only block, so the client's cursor moves on.
  - On SIGTERM the stream sends `reconnect` with `retry: 1000` (D-98).
- **Routes:**
  - `/v1/me/stream`, `/v1/admin/stream`, and `GET /v1/biz/businesses/{bid}/stores/{sid}/stream`, behind the same authz as the other biz routes.
  - The store stream re-checks permission every minute. A removed member is told to reconnect with reason `revoked`, and reopening is refused (D-107).
- **`@todayz/realtime`:** a fetch-based stream client with backoff, cursor and reset handling, and the `useRealtime` hook that invalidates queries.
- **Notify core (`internal/notify`, `internal/platform/notice`):**
  - Inbox table and API: list, unread count, mark read. There is a customer inbox and a business inbox; the business inbox is limited to the stores a staff member can see.
  - `Send(ctx, tx, Notification)` validates kind, recipient, params and ref, then writes the inbox row and an `inbox.updated` event. It queues one outside message only when every switch allows it:
    - the kind allows the channel;
    - the platform flag is on;
    - a provider is configured;
    - the phone is valid E.164;
    - the user's per-kind preference is on.
  - The phone is sealed in the delivery row, never put in the job args, and cleared when the delivery ends.
  - MSG91 SMS (flow API) and WhatsApp (bulk template API), using `SMS_API_URL` and `WHATSAPP_API_URL` through the outbound client.
  - Templates per kind and language, with an English fallback. The template's own language is what is sent.
  - Delivery status, provider id and a cost metric, with at most one send per delivery.
  - Minimal per-kind preferences, and an integration status that reports only set or missing.
- **Escalation:**
  - A River job runs every minute. It SMSes the store contact phone (or WhatsApps it, when that is on and configured), falling back to the owner's phone.
  - It fires when a new order is still unseen after `order_unseen_sms_after_min`, read from settings.
  - It sends once per order (the claim row is written in the same transaction), in the business language with an English fallback.
  - The orders and contacts come through interfaces that W2-02 and W1-04 implement. Until they land, the job is not registered.

## Evidence (agent)

| Check | Result |
|---|---|
| `make gen` | ok |
| `go vet ./...`, golangci-lint | 0 issues |
| `go test -race` with DB: realtime, authz, jobs, notify, settingslint, cmd/todayz | ok |
| `go test -race -count=3` realtime | ok |
| `make check-contract` | redocly valid; contract smoke ok |
| `@todayz/realtime` lint, typecheck, test | clean, clean, 24 passed |
| `make check` | all steps ok through check-realtime. check-infra stops at `terraform validate` because the worktree was never initialised, which needs an online `make setup`. |
| `make check-ci`, `make check-tf-static` | ok (226 self-tests; checkov 370 passed, 0 failed) |

**Load (build tag `load`).** 2,000 streams on 1,000 channels:

| Measure | Result |
|---|---|
| Connect | 484 ms |
| Heap | +68.6 MiB (35.1 KiB per stream, client and server) |
| Goroutines | 7.00 per stream |
| 1,000 events from 8 publishers | published in 244 ms; all 2,000 deliveries done in 244 ms |
| Publish to deliver | p50 710 µs, p90 1.05 ms, p99 1.55 ms, max 3.67 ms |
| Streams dropped | none |

**Tests that guard behaviour:**
- Replay after a reconnect, and no duplicate when a NOTIFY arrives late.
- A rolled-back publish delivers nothing.
- Cross-instance delivery, `reset`, and the SIGTERM reconnect.
- Store stream access:
  - owner and cashier allowed;
  - another business gets 403/404;
  - the D-107 removal ends the stream with `revoked`, and reopening is refused.
- Inbox and preferences over HTTP.
- `Send`: refusals and every skip switch. The phone is never stored in clear or put in a job.
- `Deliver` sends at most once. A failure is final and stores no phone.
- Escalation: once per order, the setting-driven cut-off, the language fallback, the WhatsApp choice, and paging.
- Integration status never shows a value.
- Provider wire formats, and failures that never carry the phone or key. The providers are refused under no-egress.

## Fixed during QA

- The inbox ops map was keyed by operationId instead of the strict server's Go method names, so every inbox call returned 500. `TestOpsCoverEveryOperation` now guards it.
- A language without an approved template sent the English template labelled with that language.
- A replayed event could arrive again live when its NOTIFY reached the hub after the replay.

## Open

1. **W2-02 and W1-04:** implement `notice.UnseenOrders` and `notice.StoreContacts`, then wire them in `jobs.EscalationSources`. `StoreContacts` belongs in W1-04 because it seals store phones with its own AAD.
2. **Founder:**
   - the MSG91 DLT template ids;
   - the WhatsApp integrated number;
   - the SMS cost per unit (0 until known);
   - a review of the Hindi templates.
3. **Lead:**
   - the `store.stream` and `admin.stream` actions;
   - shared business inbox read state;
   - a provider failure is final (no WhatsApp→SMS retry), and a crashed `sending` row stays as it is;
   - escalation can run up to one minute after the setting.

## Deferred

Web push (VAPID), the B-16 business inbox screen, and the D-101 ask push templates (only their event types are registered).

## Not verified

- Real MSG91 SMS and WhatsApp.
- `terraform validate` in the worktree.

## Lead, after the merge (2026-10-23)

Fast-forwarded `main` to 73f7aa2 (nine commits, `f83f9d3..73f7aa2`), with no rebase needed. Before merging, I re-ran the checks in the worktree myself:

| Check | Result |
|---|---|
| `make gen` | ok |
| `go vet ./...` | ok |
| golangci-lint | 0 issues |
| `go test -race ./...` (whole backend, DB tests on) | all packages ok, no failures. This includes the duplicate-event fix the agent had not run under the full suite. |
| `pnpm --filter @todayz/realtime test` | 24 passed |
| Diff scan | no added log line names a phone, OTP, token or key. The migrations are in the 205xx block (20501 outbox, 20502 notifications). No secret files. |

Decisions on the agent's open questions:

- **`store.stream` and `admin.stream`:** keep them as their own actions. Opening a stream is not the same as viewing an order. The stream carries only `{type,id,version}` and the client refetches through the normal authz'd endpoints, so the stream action leaks nothing extra.
- **`StoreContacts`:** W1-04's domain (`internal/business`) implements it, because store phones are sealed with its AAD. The business notification language comes from the business record and falls back to English. W2-02 implements `UnseenOrders`. The lead wires both in `jobs.EscalationSources` when W2-02 merges.
- **Provider failure is final; escalation up to one minute late:** accepted for launch. One outside message per order is the safer failure mode.
- **Shared business inbox read state:** accepted for now. Per-member read state is a later change if shops ask for it.
