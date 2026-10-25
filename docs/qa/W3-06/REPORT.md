# W3-06 Field agents: QA report

This covers A-04 (a field agent creates a business for a shop) and A-15 (field agent stats), on the backend and in the admin console. The branch is `claude/w3-06-field-a8ac7f`, with local `main` 6a2c248 merged in (no fetch). Everything ran locally: nothing was pushed, and no cloud account or provider was called.

## What was built

**Contract (`backend/api/{paths,schemas}/admin-field.yaml`)** These routes sit under `/v1/admin/field/businesses`:
- create, list, read and update;
- stores;
- photos (`documents`, which answers a presigned PUT);
- owner code send and verify (`owner-confirm`, `owner-confirm/verify`);
- store items: list, add one, edit;
- catalogue search per store (`products`).

Stats come from `/v1/admin/field/me/stats` (the agent's own row) and `/v1/admin/field-agents` (admins).

**Database** Migration 30601 adds two tables:
- `field_owner_codes`: only a keyed hash of each code, plus the hash of the phone it went to;
- `field_item_adds`: which agent added which item, for A-15.

`businesses.onboarded_by` (from W1-04) is the attribution. Field policy actions are in `platform/authz/policy/field.go`, with the golden file `field.golden`.

**Backend (`internal/business/agent*.go`)**
- *Ownership:* an agent sees and changes only the businesses they onboarded. Another agent's business is 403. A business the shop created itself is 404 on these routes.
- *Edit window (D-39):* items, stores and photos are open to the agent until `field_agent_edit_window_days` after verification. The number is read from `platform_settings`, the end is inclusive, and admins are not windowed.
- *Owner code:*
  - 6 digits, sent by SMS to the owner's number.
  - `submit_incomplete` (409, with the missing fields) is returned before any SMS.
  - Tries left are in `extensions.attemptsLeft`. After the last try the code is `otp_locked` (429). An old code is `otp_expired` (401). Too many sends are `rate_limited` with Retry-After.
  - A new owner number ends the old code. The owner's number cannot be the agent's own or a staff number.
  - When the right code is read out, the business goes `draft` → `submitted` with `ownerConfirmed`, and the owner can see it in the business app.
- *Items:* added one by one, either from the master catalogue or as a new product, which goes to A-08 review as `pending_review`. Adding an item already in the store is a conflict.
- *Audit:* every write is audited with the agent as an admin actor. No audit row, log line or response carries the full number or a code.
- *A-15 numbers:* onboarded, verified, stores live, products added, shops in the edit window, first-order rate, and an 8-week trend.

**Admin console**
- *A-04 (`admin/src/field/`):*
  - Routes are `/field`, `/field/new` and `/field/$businessId`.
  - The phone-first wizard has five steps: details; store (address, a "Use my location" pin or typed coordinates, phone, one opening range for the chosen days); photos (camera capture, type and size checked on the phone, PUT straight to the presigned URL); products (search, add with price and opening stock, or a new product, then edit the price inline); and owner.
  - The owner step sends the code and shows the masked number and the expiry. It has six code boxes, says how many tries are left, and shows a resend countdown.
  - On `submit_incomplete` the wizard sends the agent to the step that fixes it.
  - When the window has closed, the steps are read-only.
- *A-15 (`/field-agents`):* an agent sees only their own tiles and trend, and the page never calls `/field-agents`. Admins get a table of every agent and pick whose weekly trend to show.
- *Sidebar:* "Add shops" sits right after Pulse, and "Field agents" sits in the Growth group. Both show to `field_agent`, `admin` and `super_admin`. A field agent now starts at `/field`.
- *Width:* built for 360 px. The code boxes shrink to share the row beside the icon rail, and on a phone only every other week is labelled on the chart.

**Copy:** `shared/i18n/messages/admin/field.en.json` (166 keys) and two keys in `shell.en.json`. The admin console is English-only, so there are no hi/kn/te/ta drafts.

## Evidence

All of this ran on the merged tree, at the head this report is committed in.

| Check | Result |
|---|---|
| `make gen` | exit 0 |
| `go build ./...`, `go vet` (business, cmd/todayz) | clean |
| `go test` business, catalogue, identity, authz, authz/policy (via `TODAYZ_TEST_PG_URL_FILE`) | ok (6 packages) |
| `go test ./cmd/todayz/...`, which includes `TestFieldOnboardingHTTP`, `TestFieldOwnerCodeLimitsHTTP`, `TestFieldEditWindowHTTP` and `TestFieldStatsHTTP` ok (79 s) |
| Admin `lint`, `lint:css`, `typecheck` | clean |
| Admin unit (`vitest run`) | 25 files, 223 tests passed (12 in `src/field/field.test.tsx`) |
| Admin `build:e2e` | ok; precache check and dist-check pass |
| Admin `build` (production mode) | ok with a placeholder `VITE_TURNSTILE_SITE_KEY` and an https `VITE_API_URL` set in the shell (it refuses the local test key) |
| Admin Playwright, whole suite | 30 passed. `e2e/field.spec.ts` (3 tests) runs at 360 × 812 and fails on any sideways scroll. `e2e/roles.spec.ts` (AD-02) has the new sidebar for every role. |
| `@todayz/i18n` vitest | 4 files, 198 tests passed |

Scenarios: `e2e/scenarios/FA.md` (SC-FA-01…10), each with the tests that cover it.

## Screenshots

These are in `docs/qa/W3-06/`. They are from `TZ_QA_DIR=docs/qa/W3-06 pnpm exec playwright test e2e/field.spec.ts`, run in `admin/` after `pnpm run build:e2e`, against the faked API.

| File | What |
|---|---|
| `admin-a04-new.png` | New shop on a 360 px phone |
| `admin-a04-store.png` | Store step filled (full page) |
| `admin-a04-items.png` | Products: a product added and "Already listed" in search |
| `admin-a04-owner.png` | Owner code after a wrong code ("4 tries left"), resend counting down |
| `admin-a04-confirmed.png` | Owner confirmed, shop waiting for review |
| `admin-a04-list.png` | The agent's list of shops |
| `admin-a15-mine.png` | A-15 for a field agent: own tiles and the 8-week trend |
| `admin-a15-all.png` | A-15 for an admin at 1280 px: every agent, Lakshmi's trend picked |

The owner's number in the screenshots is a masked fixture. No real number, code or token appears in any of them.

## Files changed outside the owned paths

The owned paths are `admin/src/field`, `internal/business/agent*.go` and `backend/api/{paths,schemas}/admin-field.yaml`.

- **Backend:**
  - `backend/cmd/todayz/routes_business.go`: mounts the field routes.
  - `backend/cmd/todayz/field_http_test.go`: new.
  - `backend/internal/business/{businesses,stores,documents}.go`: the writes take the actor, and store and document inserts are shared with the field routes. There is no behaviour change for the owner routes.
  - `backend/internal/catalogue/field.go`: item adds for agents, including a new product as pending.
  - `backend/internal/identity/field.go`: account lookup for the owner number and the SMS sender.
  - `backend/db/queries/{business/agent,catalogue/field}.sql`
  - `backend/db/migrations/30601_w3-06_field.sql`
  - `backend/internal/platform/authz/policy/field.go` and `testdata/field.golden`
- **Admin:**
  - routes `admin/src/routes/field/{index,new,$businessId}.tsx` and `admin/src/routes/field-agents/index.tsx`
  - `admin/src/shell/nav.ts`: two entries and `BUILT_SECTIONS`. At the merge this conflicted with main's `support`; both lists are kept.
  - `admin/src/app/nav/access.ts` and `access.test.tsx`
  - `admin/e2e/{roles.spec,fake-api}.ts`, plus the new `admin/e2e/field.spec.ts`
  - `admin/CLAUDE.md`: the status line. The old "edit nothing in `nav.ts`" convention now says how W3-05 and W3-06 add a section.
- **Shared and docs:**
  - `shared/i18n/messages/admin/field.en.json` (new) and `shell.en.json`
  - `e2e/scenarios/FA.md` (new)

## Open

- **TODO(lead) `agent.go:727`:** 02 §3.3 gives stores and photos `admin.business.onboard` (own shops, no window). The code also applies the D-39 window to stores and photos. This is the conservative reading: an agent cannot add a store to a shop verified months ago.
- **TODO(lead) `catalogue/field.go:189`:** 01 §5 does not say whether a store item of a pending product is visible to customers before A-08 approves it. The code follows the search rule, which shows active products only.
- **TODO(lead) `identity/field.go:7`:** the owner code reuses the sign-in DLT template text. A dedicated "confirm your shop on TodayZ" template has to be registered with DLT first.
- **Admin CSP:** the admin console has no CSP yet. When it gets one, `connect-src` needs the S3 or MinIO upload host for the photo PUT, and Permissions-Policy needs `geolocation=(self)` for "Use my location".
- **Photos:** photos are attached to the business's first store only. With several stores the wizard does not ask which one.
- **Hours:** the wizard has one opening range for all chosen days. Split hours or different hours per day need A-05 or the business app.
- **Stores:** a store cannot be edited from the wizard after it is created, because the contract has no field store update. A-05 can edit the pin and hours.
- **Owner-code expiry:** the code response has no `serverNow`, so the expiry is shown as an IST clock time from `expiresAt` rather than as a relative "in 5 minutes".
- **Error codes:** the contract keeps sign-in's codes for the owner code: `otp_locked` 429 and `otp_expired` 401.

## Questions for the lead

1. Should the edit window apply to stores and photos (as built), or only to items (02 §3.3 as written)?
2. Can customers see an item of a pending product before A-08 approves it? It is hidden for now.

## Not verified

- A real SMS to the owner's phone: no provider is called in worker sessions. Locally, the owner code goes to the log-only sender, and reserved test numbers take 000000.
- Photo capture with a real phone camera, and "Use my location" on a device. Both were checked only with a set file and typed coordinates.
- The journey against the running stack (`make dev`): the Playwright journeys run against a faked API. The real API is covered by the Go HTTP tests.
- `make check`, `make e2e`, `make reset`: these are not run in worker sessions.
