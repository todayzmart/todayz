# Auth refresh replay (D-239) and A-09 customers with no wallet (D-240)

Fixes for W4-01 "Bugs found" items 7 and 2 (`docs/qa/W4-01/REPORT.md`). Branch `worktree-agent-a81f47be05cf293ae`, based on main 2f0384db: commits 8c37fe9b (D-239) and 0ad5e736 (D-240). Run locally on 26 Oct 2026.

## 1. Lost refresh response (D-239)

**Cause.** Each refresh revoked the presented session row and inserted a successor with a random token. When the response was lost, the cookie still held the old token:
- inside the 30 s `RotationGrace`, the client's retry got a 401 and the client signed the person out;
- after the window, the same retry counted as reuse, so the whole family was revoked and audited as theft.

**Fix.**
- Migration `49803_w4-98_session_successor.sql` adds `successor_id` and `successor_salt` to `sessions`, with check constraints.
- The successor token is derived: HMAC under `PII_HASH_KEY` of the presented token plus a random 32-byte salt. Only its SHA-256 is stored.
- A retry inside the window whose successor is still live gets that same token back. Live means the successor:
  - has never been presented;
  - is in the same family, user and app;
  - matches by hash (constant-time compare).
- The replay writes nothing and audits nothing. It runs the same checks as a refresh: expiry, blocked or deleted user, disabled admin, TOTP `amr`.
- Unchanged:
  - a used successor still gets the grace 401;
  - past the window it is still reuse, and the family is revoked.
- Lock order is the users row, then the presented row, then the successor row, so concurrent retries end with one token.

**Changed files.**
- `backend/internal/identity/session.go`: `Refresh`, `successorToken`, `replaySuccessor`, `successorSaltLen`.
- `backend/db/queries/identity/identity.sql`:
  - `RotateSession` replaces `RevokeSession`;
  - new `GetSessionForUpdate`;
  - successor columns added to `GetSessionByRefreshHashForUpdate`.
- Docs: 02 §3.1, 03 (sessions), 04 (refresh), 12 (D-239, and a note on D-105).
- `e2e/fixtures/journey.ts`: comment only.

**Tests.**
- `backend/internal/identity/refresh_replay_test.go` (7 tests):
  - a lost response;
  - the successor already used;
  - past the grace window;
  - a successor that is not live;
  - blocked user and admin checks;
  - 12×4 concurrent replays;
  - a replay racing the successor's own refresh.
- `TestRefreshRotationGrace` is removed.
- `TestIdentityHTTPRefresh` is updated.

## 2. A-09: a customer with no wallet (D-240)

**Cause.** A ledger account opens only at a customer's first posting (`Service.accountID`). The A-09 search listed accounts only, and an adjustment needed `accountId`. A first-time customer could be neither found nor credited.

**Fix.**
- **Search.** `GET /v1/admin/ledger/accounts`, by id or phone, also returns a `customer_wallet` row with `id: null` and a ₹0 balance for such a customer. A search opens nothing.
- **Adjustment.** `CreditAdjustmentCreate` takes exactly one of `accountId` or `customerId`; both or neither is a 422.
  - On the customer path, the D-237 advisory lock is taken before the wallet lookup.
  - With no wallet, the user must be live and have a customer profile (`IsCustomer`).
  - An at-once posting opens the wallet in the same transaction through `Post` (`InsertAccount ON CONFLICT DO NOTHING`).
  - A pending request opens nothing; approval opens the wallet.
  - A debit against a wallet that does not exist is `adjustment_insufficient_balance`.
  - Idempotency keys and the day sum are unchanged. With no wallet, only requests count toward the day sum.
- **Admin UI.**
  - A null-id row opens `UnopenedPanel`: ₹0, "No ledger lines yet" and Adjust. It is held in component state, because the URL carries account ids only and never a phone.
  - `AdjustDialog` sends `customerId` when the account id is null.
  - After an adjustment the search runs again, so the row gets its new account id.

**Changed files.**
- `backend/internal/credits/adjustments.go`, `adjustments_handlers.go`.
- `backend/db/queries/credits/adjustments.sql`.
- `backend/api/schemas/admin-credits.yaml`: `AdminLedgerAccount.id` is nullable; `customerId` added. `backend/api/paths/admin-credits.yaml`: descriptions only.
- `admin/src/credits/CreditsPage.tsx`, `AdjustDialog.tsx`.
- `shared/i18n/messages/admin/credits.en.json`: `credits.panel.unopened`.
- Docs: 04 (two rows), 06 (A-09), 12 (D-240).

**Tests.**
- `backend/internal/credits/adjustments_unopened_db_test.go`:
  - `TestLedgerAccountsUnopenedWalletDB`;
  - `TestAdjustmentOpensWalletDB`;
  - `TestAdjustmentUnopenedWalletApprovalDB`;
  - `TestAdjustmentUnopenedWalletRefusalsDB`;
  - `TestAdjustmentUnopenedWalletConcurrentDB`.
- A new admin vitest case in `admin/src/credits/credits.test.tsx`.

## Results

- **D-239:** `go test -p 2 -count=1 ./internal/identity/... ./internal/platform/migrate/... ./cmd/todayz/...` passes against the shared testpg.
- **D-240:** `go test -p 2 -count=1 ./internal/credits/... ./cmd/todayz/...` passes, including `TestContractSmoke`.
- golangci-lint on identity, credits and cmd/todayz reports 0 issues; gofmt is clean; `go build ./...` passes.
- Admin: `tsc --noEmit` and eslint are clean; vitest passes 286/286; `make check-i18n-messages` reports 0 errors.
- Not run on the branch: `make check` and `make e2e`. They run in the lead's release candidate.

## Open questions for the lead

1. **D-239 replays rather than re-rotating.** Re-rotating would mint a new token on each retry, so concurrent retries would raise the same false alarm 30 s later. The cost of replaying: a thief who presents a copied token within 30 s of the owner's rotation, before the owner uses the successor, also gets the successor. Detection is delayed, not lost.
2. **A used successor presented inside the window still gets the grace 401**, not reuse plus a revoke.
3. **D-240 "customer"** means not deleted and with a `customer_profiles` row, matching admin's `is_customer`.
4. **A pending request for a customer with no wallet opens nothing until it is approved.** The requester sees ₹0 until then.
