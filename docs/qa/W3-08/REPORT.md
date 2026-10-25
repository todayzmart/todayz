# W3-08 Support: QA report

W3-08 adds support tickets: the backend in `backend/internal/support`, the admin screen A-14 (ticket inbox), the business screen B-15 (help and tickets) and, picking up what W2-09 deferred, the customer screen C-19 (help and tickets).

- Branch: `claude/admiring-mcnulty-ba5cee`: the W3-08 commit `b45d936` on base `0776c04`, then `main` (`1465e58`: W3-02, W3-03, W3-10) merged in and the follow-up below. Not pushed.
- Everything ran locally: testcontainers Postgres, MinIO through the existing presigner, Playwright against a faked API. Nothing touched a cloud account.
- Decision: D-226. Rules: 01 §13 and §17. Scenarios: 08 SC-SP-01…08.

## What was built

### Schema and settings

- **Migration `30801_w3-08_support.sql`** (block 30801–30899, D-89) creates three tables:
  - `support_tickets`: business or customer opener, status, priority, `sla_started_at` and `sla_due_at` (set exactly while `open`, by CHECK), `first_response_at`, assignee, and the store and order links (the D-211 FK);
  - `support_messages`: reply or note, from the opener or support;
  - `support_attachments`: object key, type, size, uploader, claimed by one message.
- **SLA timers** are `platform_settings` keys, in minutes, min 15, max 10080:
  - `support_sla_urgent_min` 60
  - `support_sla_high_min` 240
  - `support_sla_normal_min` 1440

  The due time is read when it is set. Nothing is hard-coded. The timer starts on opening or on an opener reply that reopens the ticket, and stops on an agent reply, resolve or close. Past due shows as overdue; nothing escalates.

### Backend (`internal/support`)

- **Business API** (`/v1/biz/businesses/{bid}/support/*`, `support.tickets`: owner and manager):
  - list (cursor);
  - open, with a category, subject, body, an optional store and order of the same business (otherwise 422), and attachment ids;
  - thread (notes never included);
  - reply (a `waiting` or `resolved` ticket reopens with a new due time; a `closed` one answers 409 `invalid_transition`);
  - register an attachment, which returns a presigned PUT.

  Support's messages carry no agent name.
- **Customer API** (`/v1/me/support/*`, `me.support.tickets`: any signed-in customer):
  - list (cursor), open (topic, subject, message, an optional order that must be the customer's own and brings its store, attachment ids), thread (no notes), reply (reopens a `waiting` or `resolved` ticket; `closed` → 409), register an attachment (under `support/customers/`).
  - Every read is scoped to tickets the caller opened as a customer: another customer's ticket, or a business ticket the same person opened as owner, is 404. A business upload cannot go on a customer ticket, nor the reverse.
- **Admin API** (`/v1/admin/tickets*`, `admin.support.tickets`: support, admin, super_admin):
  - inbox per status with counts and overdue, soonest due first, with filters;
  - thread with notes;
  - reply (the ticket moves to `waiting`, `first_response_at` is set once, and an unassigned ticket is assigned to the replying agent);
  - note;
  - assign and unassign;
  - change the priority: on an `open` ticket the due time becomes `sla_started_at` plus the new minutes (it can turn overdue at once); on `waiting` or `resolved` only the next timer changes; `closed` → 409; the same priority writes nothing;
  - resolve and close, each with a reason.

  Assign, priority, resolve and close each write one `audit_log` row in the same transaction.
- **Attachments:**
  - JPEG, PNG, WebP or PDF, at most 10 MB, at most 5 per message.
  - The presigned PUT signs Content-Type and Content-Length and sets `Cache-Control: no-store`, into the private bucket (MinIO locally).
  - An upload is claimed once, only by its uploader (and, for a business, by the same business).
  - Reads are presigned GET links valid for 15 minutes.
- **Contract:** `backend/api/{paths,schemas}/{admin-support,biz-support,support}.yaml`.
- **Policy golden:** `authz/policy/testdata/support.golden`.
- **Seed:** business and customer tickets (Asha's and Ravi's), so the inbox and C-19 both have data.

### A-14 Support inbox (`admin/src/support/`)

- Status tabs with counts, an overdue chip and minutes counted from `serverNow`, never the device clock. Filters are kept in the URL.
- The thread shows notes marked internal.
- The composer handles reply and note, attachments through the presigned PUT, and an `Idempotency-Key` on every write.
- Assign to me or to a chosen agent. Change the priority (the SLA chip moves with it). Resolve and close need a reason. A closed ticket takes only notes.
- `<AssistSlot/>` is mounted for W3-13, only while triage assist is on.
- New nav entry and READS rows in `e2e/roles.spec.ts` (AD-02).

### B-15 Help (`business/src/help/`; routes `/help`, `/help/new`, `/help/$ticketId`)

- **Every role:**
  - "How TodayZ works for your shop" (six steps);
  - seven FAQs, written to 01 and naming no configurable number;
  - how to reach support.
- **Owner and manager get a Tickets tab.** A cashier sees a note to ask the owner or a manager and makes no ticket calls (the API answers 403 too).
- **New ticket:**
  - category, subject (3–120), shop (defaults to the remembered or only store), order (the store's recent done, missed and accepted orders), message, and up to 5 photos or PDFs;
  - each file is checked in the browser (type, empty, size, count), registered, then PUT with only `Content-Type`;
  - an `Idempotency-Key` on the create;
  - a 422 shows which field was refused.
- **Thread:**
  - support shows as "TodayZ support", and files as links;
  - the reply box says when a reply reopens the ticket;
  - a closed ticket shows "Open a new ticket" instead of a reply box;
  - another business's ticket shows "Ticket not found".
- **Copy:** `shared/i18n/messages/business/help.en.json` (118 keys) and `shell.nav.help`, in hi, kn, te and ta, approved under the founder's standing instruction (see "After the merge").
- **Nav:** a sidebar entry "Help" (Lucide `LifeBuoy`), last in the Shop group, so phones list it under More.

### C-19 Help (`customer/src/help/`; routes `/help`, `/help/new`, `/help/$ticketId`)

- Signed out, every route asks to sign in and comes back after.
- Entry points: a Help row on Account ("Get help or see your tickets") and "Get help with this order" on C-12, which opens `/help/new?orderId=…` with that order and the topic "An order" preselected.
- **List:** the customer's tickets, newest activity first, with status and "x ago" counted from the response time.
- **New ticket:** topic, an optional order (the customer's recent orders), subject, message, up to 5 photos or PDFs (checked in the browser, registered, then PUT with only `Content-Type`), one `Idempotency-Key` kept across retries.
- **Thread:** "You" and "TodayZ support" (never an agent's name), files as links, a state note per status; the reply box says when a reply reopens the ticket; a closed ticket offers "Open a new ticket"; someone else's ticket shows "Ticket not found".
- No reply time is shown (the SLA is an internal target).
- **Copy:** `shared/i18n/messages/customer/help.en.json` (81 keys), in hi, kn, te and ta, approved under the founder's standing instruction. Initial JS is 140.84 KB of 170 KB gzip.

## Scenario coverage

| Scenario | Backend (I) | A-14 | B-15 | C-19 |
|---|---|---|---|---|
| SP-01 open a ticket, store/order not the opener's → 422 | `TestOpenBusinessTicket`, `TestOpenCustomerTicket` | — | unit: create with key, store, order; 422 field. e2e: desktop journey | unit: open from an order, validation, 503 retry keeps the key; e2e: from C-12 at 360 px |
| SP-02 SLA from settings, overdue from `serverNow` | `TestSLAFromSettings` | unit: SLA chip from serverNow; e2e | — | (no timer shown) |
| SP-03 agent reply → waiting, first response, auto-assign; B-15 shows "TodayZ support" | `TestAgentReply` | unit + e2e | unit: thread; e2e: support reply | unit: thread reply; e2e: support reply shows "TodayZ support" |
| SP-04 opener reply reopens; closed → 409; reasons required | `TestOpenerReplyAndClose` | unit: resolve needs reason, closed takes notes | unit: reply, 409 message, closed has no box; e2e: reply reopens, closed ticket | unit: closed ticket offers a new one; e2e: reply reopens, closed ticket |
| SP-05 notes hidden from every `/v1/biz` read | `TestNotesHidden` | unit: notes marked | (business fixtures carry no notes) | `TestCustomerThread` (note not in the thread) |
| SP-06 assign, priority and audit | `TestAssignAndAudit`, `TestSetPriority` | unit: assign to me and to an agent, change priority; e2e: priority | — | — |
| SP-07 attachments: type, size, count, claim once, presigned | `TestAttachments`, `TestPresigner` | unit: upload then claim | unit: register then PUT with only Content-Type; type and empty refused. e2e: photo on a new ticket | `TestCustomerAttachments`; e2e: photo PUT to MinIO host |
| SP-08 permissions and isolation | `TestBusinessIsolation`, policy golden | AD-02 `roles.spec.ts` | unit: cashier sees no tab, no calls; 404 not found. e2e: cashier at 360 | `TestCustomerIsolation`; unit + e2e: signed out, not found |

## After the merge (lead's answers, 25 Oct)

- **Translations.** The founder's standing instruction ("go live with hindi", then "and all other languages too") covers the help copy. All 118 business and 81 customer `help.*` keys, and `shell.nav.help`, are marked `review: reviewed` in hi, kn, te and ta with `translations.mjs approve`, the same way W3-04 did. Records: a row and a "strings to check" table in `docs/qa/business-i18n/REVIEW.md`, and a row in `docs/qa/W4-05-hindi-customer/REVIEW.md`. No native-speaker review.
- **Sentence case.** Main's new copy check (`shared/i18n/test/i18n.test.ts`) flagged six English help strings that named screens or examples in title case. They now quote them (“Orders”, “Sell”, “Credits”, “Payout for March not received”, “Atta missing from my order”). The code FAQ now quotes the customer button as it really reads, “Get a new code” (it said “New code”), and each language copies the customer app's own `orders.code.new` text into the quotes. The changed keys were imported again and approved.
- **Business nav.** Lead-approved: `business/src/shell/nav.ts` has "Help" (`LifeBuoy`, sidebar only, Shop group); `Shell.test.tsx` counts it as a built section.
- **Support phone.** As built: hidden while `VITE_SUPPORT_PHONE` is unset. The lead sets it on Cloudflare when the founder gives a number.
- **Priority.** The lead accepted the reading (on `open`, due = `sla_started_at` + the new minutes, can be overdue at once; on `waiting` or `resolved` only the next timer changes); recorded in D-226.
- **Merge fixes.**
  - The A-17 settings test from main requires every key to have a 01 §17 group, so `support_sla_*` now has a `support` group: the `AdminSettingGroup` enum in `backend/api/schemas/admin-settings.yaml`, `groupPrefixes` and `groupOrder` in `internal/admin/settings.go`, the A-17 label in `admin/src/settings/labels.ts` and `settings.group.support`.
  - The owner shell now reads `GET {S}/overview` (W3-03), so `business/e2e/help.spec.ts` answers it, as `sell.spec.ts` does.

## Checks run (area checks only)

Run on 25 Oct after the merge, offline, against testcontainers Postgres and faked APIs. The e2e builds were rebuilt first (`build:e2e` in admin, business and customer).

| Area | Command | Result |
|---|---|---|
| Contract | `make gen`, `make check-contract-lint check-contract-client` | gen ok; redocly valid; api-client 42/42 |
| Backend | `gofmt -l`, `go vet ./...` | clean |
| Backend | `go test -race ./internal/support/... ./internal/platform/authz/... ./internal/seed/... ./internal/platform/settings/... ./internal/insights/... ./internal/admin/... ./cmd/todayz/...` | all ok (cmd/todayz 149 s, including `TestRoutesAreInTheContract` and the contract smoke); `internal/admin` rerun after the settings group fix |
| i18n | `pnpm run gen`, `pnpm run lint:messages`, `pnpm -C shared/i18n exec vitest run` | 0 errors in admin, business, customer, site; business 1666/1666 reviewed per language; vitest 198/198 |
| Admin | `typecheck`, `lint`, `vitest run` | clean; 211/211 |
| Admin | `playwright test` (all specs, incl. support, roles, risk) | 27/27 |
| Business | `typecheck`, `lint`, `vitest run` | clean; 424/424 (`language.test.tsx` passes) |
| Business | `playwright test e2e/help.spec.ts e2e/sell.spec.ts e2e/standee.spec.ts e2e/smoke.spec.ts` | 18/18 |
| Customer | `typecheck`, `lint`, `lint:css`, `vitest run`, `size` | clean; 142/142; initial JS 140.84 / 170 KB |
| Customer | `playwright test e2e/help.spec.ts` (360 px) | 3/3 |

Not run (outside the area-check rule): the full `make check`, `make e2e`, load and security suites.

## Screenshots

- `admin-a14-inbox.png`: A-14 open tab with counts, SLA chips and filters.
- `admin-a14-ticket.png`: A-14 thread with an internal note and the composer.
- `business-b15-guide-desktop.png`: B-15 guide and FAQs (owner), with "Help" active in the sidebar's Shop group.
- `business-b15-new-ticket-desktop.png`: B-15 new ticket with a store, an order and an attached photo.
- `business-b15-thread-desktop.png`: B-15 thread after support's reply ("TodayZ support", the reopen hint).
- `business-b15-tickets-360.png`: B-15 tickets list at 360 px.
- `business-b15-cashier-360.png`: B-15 for a cashier at 360 px (guide only).
- `customer-c19-new-ticket-360.png`: C-19 new ticket from an order, with a photo, at 360 px.
- `customer-c19-thread-360.png`: C-19 thread after support's reply, with the reopen hint.
- `customer-c19-tickets-360.png`: C-19 ticket list from Account.
- `customer-c19-closed-360.png`: C-19 closed ticket offering a new one.
- `customer-c19-signed-out-360.png`: C-19 signed out.

## Open items for the lead

Items 1–3 of the first report (translations, the nav entry, the support phone) are settled; see "After the merge".

1. **A-14 audit panel.** A-14 does not yet embed the shared `src/audit/` side panel for a ticket's audit rows. The rows are written and visible in A-19.
2. **Reply time.** B-15 and C-19 say "Support will reply here." and show no reply time: the SLA is an internal target (01 §13 now says so). If the founder wants a promise shown, it is a copy change reading the same settings.
3. **Notify.** Telling the opener about a reply is `internal/notify`'s (D-226 Open); W3-08 sends none.
4. **Presigner.** `internal/support/presign.go` is a copy of `internal/business`'s until it moves to `platform/storage` (`TODO(lead)`).
