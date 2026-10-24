# W2-11 POS counter and offline: QA report

B-05 Sell in `business/`: the counter, an offline queue with sync status, a barcode scanner and the receipt. Branch `claude/wp-w2-11-pos-ui-7d734e`, from `7f0838e` to the head of the branch (see `git log`). W2-04 reached main in `65353e9`; main was merged in `7e441c2`, then `make gen` was rerun. The only conflict was `docs/plan/12-decision-log.md` (both sides appended; both kept). `backend/api` is identical to the W2-04 branch the adapter was built against, so there was no contract drift and no business code changed. Everything ran locally; nothing touched a cloud account, and `make dev`, `make e2e` and `make check` were not run (lead's rule).

## What was built

- **Counter (B-05):** quick tiles (alphabetical until a best-seller feed exists), search by name or barcode, custom item, cart with a quantity stepper, whole-bill and line discounts as the server returned them (manager and above), payment kinds (cash, UPI, card), Charge with F2 and F4 shortcuts, Clear bill, a short-stock banner. Phone layout: a bill bar that opens the bill sheet.
- **Offline queue:** every sale is written to IndexedDB with a client ULID and the next device sequence before it is shown as sold. Online it is sent alone (`createPosSale`); offline, or after a failure, it goes through `syncStore` in sequence order with backoff. Duplicates settle as already applied; a refusal stays in the list as "needs your attention" with the server's reason. Sales apply even when stock goes negative.
- **Customer links:** bill QR (an in-repo QR encoder, no dependency), counter code, unverified phone. Any counter-code lock is one generic locked state.
- **Scanner:** keyboard wedge always; camera through `BarcodeDetector` when the browser has it (no new dependency). An unknown barcode opens "Add this product".
- **Receipt:** from the local or the server sale, so it works offline; browser print at 80 mm; share link when the server gave a token.
- **Sales sheet:** the sales on this device with their sync state, Receipt, and manager-only Reverse with a reason.

## Evidence

| Check | Result |
|---|---|
| business typecheck | clean |
| business eslint (`--max-warnings 0`), stylelint | clean |
| business vitest | 358 passed in 41 files (engine 34: replay, duplicates, failure, conflict and refusal; API adapter 10; Counter 17) |
| `@todayz/i18n` test | 197 passed |
| `build:e2e` | ok; precache and dist checks pass |
| size-limit | initial JS 159.38 kB gzip (limit 250), CSS 10.35 kB gzip (limit 20) |
| `e2e/sell.spec.ts` (Playwright, preview of the e2e build, faked API) | 3 passed; no console errors, no non-local requests |

All rerun after the merge with main; same results (358 and 197 tests, 159.38 kB, `build:e2e` ok).

## Screenshots

`docs/qa/W2-11/`:

- `sell-counter-desktop.png`: a bill of four items, UPI chosen.
- `sell-counter-phone.png` and `sell-bill-sheet-phone.png`: bill bar and bill sheet at phone width.
- `sell-offline-queue.png`: offline, two sales saved on the device, the sales sheet with both "Waiting to sync".
- `sell-receipt.png`: the receipt after a charge.

Seen in the offline shot: the shell's offline banner and the counter's own "Offline · 2 sales waiting to sync" notice both show. The shell banner is outside this work package; left as is.

## Adapter's assumed endpoint shapes

`src/pos/api.ts` uses the generated client only: `syncStore` (cursor, entries; results per entry, `changes`, `cursor`, `hasMore`, `serverNow`), `createPosSale`, `reversePosSale`, `createBillClaim`, `getBillClaim`, `verifyCounterCode`, `listStoreItems` and `getStoreItem`. Server errors it acts on: `price_changed`, `claim_mismatch`, `claim_expired`, `claim_already_used`, `code_invalid`, `code_locked`, `reversal_window_passed`, `below_credit_minimum`, `resync_required`. Discounts are shown as returned: a line's discount is the offer plus the manual part, the bill discount is the manual bill part plus the bill offer. `expectedTotalPaise` is not sent.

## Files changed outside the owned paths

- `shared/i18n/messages/business/pos.en.json` (new, 175 keys; my own keys).
- `docs/plan/12-decision-log.md` (D-170 to D-179).
- `business/CLAUDE.md` (status note).
- `business/src/shell/Shell.test.tsx` (`sell` added to the set of built sections, since `/sell` is no longer a placeholder).
- `business/e2e/sell.spec.ts` and `docs/qa/W2-11/` (evidence).

## Open items

1. The camera scanner depends on `BarcodeDetector`; a fallback (ZXing) is a new dependency and needs a decision (D-177).
2. Raw IndexedDB instead of Dexie (D-88); confirm (D-170).
3. `clearCachedPosStore` is not wired to sign-out (D-179).
4. The add-product link from an unknown barcode goes to Stock until W2-12 offers a deep link.
5. QR test fixtures were generated with `qrencode`; the encoder is checked against them, not against a second implementation.
6. The shell's offline banner and the counter's notice duplicate each other.
7. `e2e/smoke.spec.ts` has 4 failing tests on this branch (Overview on phone and desktop, the Overview tab, the More tab): the app redirects to sign-in because the spec does not set the `todayz.hasSession.business` hint the session needs before it tries a refresh. I did not change that spec or `shared/auth`; the fix is one `addInitScript` in the spec (as `sell.spec.ts` does). Needs its owner. Running it with `TZ_QA_DIR` also writes its own shots into the QA folder, so run it without that variable.

## Questions

All are marked `TODO(lead)` in code or in D-170 to D-179.

1. No snapshot or changes endpoint: the bootstrap syncs from cursor 0 and then pages `listStoreItems`. May a pruned store's cursor-0 sync return 410 on first use?
2. No settings feed for `credit_min_order_value_paise`, `pos_reversal_window_hours` and `pos_offline_link_max_hours`; the client shows the server's refusal instead.
3. The bill-QR URL `…/scan/{token}` and the customer scan route are assumed.
4. No best-seller rank feed for the quick tiles.
5. Product, category and offer change entities are ignored by the POS.
6. A counter code verified online that later falls back to sync is refused as used, and the bind window is 2 minutes; is that the intended behaviour?
7. A bill-QR sale that fails online after the claim expired is refused and must be rung again.
8. On `price_changed` the sale stands at the quoted price and the line is redrawn on the next sync.
9. Round-off for a bill under 50 paise is not specified.
10. The commit trailer is Claude Sonnet 5.5 (this session's model); the task named Opus.
11. The print size is a fixed 80 mm by 297 mm because `auto` height is not valid CSS, so a long receipt may paginate.

## Not verified

- A real API: the contract was used through the generated client and faked in tests; W2-04 was merged from its branch, not from main.
- A real camera, a real thermal printer and a hardware barcode wedge.
- Real IndexedDB eviction and quota behaviour on a phone, and a service-worker restart in the middle of a send.
- `make check` and `make e2e` (not run, per the lead).
