# W2-10 finish: onboarding (B-02), stores and staff (B-13), settings (B-14), standee: QA report

Branch `claude/wp-w2-10-finish-221990`, in `business/` (plus message files in `shared/i18n/`). Everything ran locally and offline; no cloud account, no `make dev`, no stack e2e. API calls in tests and screenshots are faked at `fetch`. Commits carry `Co-Authored-By: Claude Sonnet 5.5`; the task text said Opus 5.5, so the trailer does not match the brief.

## What landed

| Screen | Route | Source | State |
|---|---|---|---|
| B-02 Onboarding | `/onboarding` | `src/onboarding/` | Generated client. One resumable page: business, first store, hours, payout and tax, documents, send for review. |
| B-13 Stores and staff | `/stores`, `/invite/$token` | `src/stores/` | Generated client. Store list, add and edit, staff list, invite, edit, remove, accept an invitation. |
| B-14 Settings | `/settings` | `src/settings/` | Generated client. Store profile, hours, pickup and delivery, rewards and orders, payout and tax. |
| QR standee | `/standee` | `src/standee/` | Preview and download of the A5 PDF. **Adapter** (`credits/api.ts` `getStandeePdf`): `getQrStandee` is not generated yet. |

- Roles: owner does everything; manager sees stores and settings, edits them, invites and removes cashiers, mutes asks, sees payout masked; cashier gets a no-access state and nothing is fetched (D-201).
- Edits send `If-Match`; a 412 refetches. Create, invite, submit and document calls carry an `Idempotency-Key` (D-202).
- Documents are checked in the browser (type, 10 MB, not empty), registered, then PUT to the presigned URL with only `Content-Type` (D-203).
- Settings: radius 100 to 20 000 m, money limits, cashback 0 to 20% in 0.5% steps with a worked example (floor of base x bp / 1 000 000), D-41 in-store-only banner.
- Decisions: D-200 to D-205 in `docs/plan/12-decision-log.md`.

### Adapters after the merge with main

| Adapter | State |
|---|---|
| `credits/api.ts` `getBusinessReferrals` | **Swapped** to the generated `@todayz/api-client/gen/referrals/referrals`. The stats are now `clicks, pending, rewarded, creditsEarned` (no per-state credits), so the card shows counts for pending and rewarded and one "Credits earned" line. |
| `credits/api.ts` `getCreditAccount` | Pending: not in the generated client. |
| `credits/api.ts` `getStandeePdf` | Pending: `standee.yaml` is still an empty stub. Used by `StandeeCard` and the new `/standee` page. |
| `stock/api.ts` `createProduct`, `createItemImport`, `applyItemImport` | Pending: not in the generated client. |
| `credits/StatementsSlot.tsx` | W3-01 placeholder, not mine. |

### Files outside my own paths that I touched (lead authorised the adapter swap)

- `business/src/credits/{api.ts, queries.ts, ReferralCard.tsx, StandeeCard.tsx, credits.test.tsx}`: referrals swap; the standee link is now a typed `Link` to `/standee`.
- `business/src/stock/ImportSheet.tsx`: the i18n lint failed on main (`stock.import.field` and `stock.import.code` were built from a prefix); it now looks the ids up from literal maps. No behaviour change.
- `business/src/shell/Shell.test.tsx`: `settings` now has a page, so it leaves the placeholder list.
- `shared/i18n/messages/business/credits.en.json`: referral stat messages for the new shape.
- `shared/i18n/test/i18n.test.ts`: `WebP` added to the proper nouns.
- Not touched: `nav.ts` (Reviews and Credits entries intact), `business/src/{pos,sync,scanner,receipt}`.

## Evidence

Run from `business/` unless noted, after the last code change.

| Check | Result |
|---|---|
| `pnpm run lint` (eslint, max-warnings 0) | clean |
| `pnpm run lint:css` | clean |
| `pnpm exec tsc --noEmit -p tsconfig.json` | clean |
| `pnpm exec vitest run` | 231 tests, 32 files, all pass (stores 13, settings 10, onboarding 12, standee 3, plus unit tests for kyc, documents, delivery, perm, storeValues, hours; W2-12 and shell suites included) |
| `pnpm run lint:messages` in `shared/i18n` | business: 0 errors (it had 2 on main) |
| `pnpm test` in `shared/i18n` | 197 pass (sentence-case lint on the new keys) |
| `pnpm run build:e2e` | dist ok, precache-check (66 chunks) and dist-check pass |
| `pnpm run size` | initial JS 155.85 kB gzip (limit 250 kB); initial CSS 10.35 kB (limit 20 kB) |
| `pnpm run build` (production mode) | not run: needs a real `VITE_TURNSTILE_SITE_KEY`; `build:e2e` covers the same code |
| `make check`, `make e2e` | not run (low-RAM rule) |

## Screenshots

`docs/qa/W2-10-finish/`, 390 px wide (2x), from `vite preview --mode e2e` with a faked API: `onboarding-390.png`, `stores-390.png`, `settings-390.png`, `standee-390.png`. The staff rows read "New team member" because the fake data had no names. The standee preview frame is blank in headless Chromium (no built-in PDF viewer); the fake PDF is a stub. The bottom tab bar floats mid-page in the full-page captures; that is the capture, not the layout.

## Skipped (as agreed)

QR scan, dispatch, delivery, returns, accept-part and the Asks tab (orders, not this task); language step, starter catalogue, notification prefs, photos and custom delivery hours in onboarding and settings; field-agent confirm (W3-06); B-15 and B-16. The map pin picker is latitude and longitude inputs plus "Use my location".

## Open items

- `getQrStandee`, `getCreditAccount`, `createProduct`, `createItemImport`, `applyItemImport` are not in the generated client, so those screens still run on adapters.
- The preview shows nothing real until the backend serves the PDF; the screen is tested with a fake blob only.
- Storage upload (the presigned PUT) is unverified against live MinIO.

## Questions for the lead

1. `/invite/$token`: 04 has no get-invitation-by-token route. I treat the token as the staff id and show that invitation first, then the person's others. Right?
2. A failed document PUT leaves a `pending` document using one of the five slots, and 04 has no confirm or delete route. Add one?
3. The delivery radius maximum (20 km) is fixed in the API schema, not read from `platform_settings`. Should it be configurable?
4. The server requires a category to submit, but the form leaves it optional so the person can save a draft. The submit error lists it in plain words. OK?
5. A manager scoped to some stores cannot add a store (the backend marks this `TODO(lead)`). I hide Add a store for them.
6. Who owns `standee.yaml`: its header says W3-04, D-51 gives the PDF to W2-10?
7. The old `shop.invite.*` keys in `auth.en.json` are unused now. Remove them?

## Not verified

- Any call against a real backend or storage; the browser cannot set `Content-Length` on the PUT, so the signed length header is not sent.
- Hindi, Kannada, Telugu and Tamil text for the new keys (English only).
- Offline behaviour of these screens.
