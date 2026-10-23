# W1-05 Catalogue and stock: QA report

This is the weekend slice from the lead's brief: the schema, the stock writer with D-99 batch allocation, the reconcile job, batches, 10 catalogue operations, the seed and property tests. Branch `worktree-agent-a008bbec69dfe4d40`, built on `main` 3f8fb03 and rebased onto 3c6b020 (with W2-05) by the lead. Everything ran locally.

## What was built

- **Schema** (`backend/db/migrations/10501_w1-05_catalogue.sql`):
  - categories (seeded, with tints, icons and names in 5 languages as drafts), brands, master products and barcodes;
  - store items, the movement ledger and stock batches, with `stock_movement_batches` holding the per-batch allocation;
  - `catalogue_changes`;
  - the partial index `stock_batches (store_item_id, expires_on) WHERE quantity > 0` and the index `stock_movement_batches (batch_id)`;
  - counters (`on_hand`) kept by trigger.
  - The sqlc queries are in `backend/db/queries/catalogue/catalogue.sql`.
- **D-99 allocation** (`internal/catalogue/alloc.go`) as pure functions:
  - a sale takes unexpired dated batches by earliest expiry, then undated batches, then stock in no batch, then expired batches;
  - damage and downward counts take the earliest expiry first;
  - reversals restore the source batches;
  - stock in goes into a new batch, capped so Σ batch ≤ max(on_hand, 0).
- **Movement writer** (`writer.go`): Sell, Reverse, AddStock, Remove (capped at available), Count and WriteOffBatches.
  - Lock order: the stores row, then the item `FOR UPDATE`, then its batches.
  - A reused client id replays the original movement.
  - A positive count creates a "found in count" batch.
  - A count equal to on hand writes no movement and confirms freshness.
- **Reconcile River job** (`reconcile.go`, registered in `internal/jobs/catalogue.go`):
  - repairs on_hand and batch drift, writing system audit rows;
  - `batch_over` (batches holding more than on hand) is reported, never trimmed.
- **Batches:** `GET {S}/batches` and `PATCH {S}/batches/{batchId}`.
  - PATCH takes cost and expiry only, needs manager or above and is audited.
  - If-Match: 428 when missing, 412 when stale.
  - No endpoint creates a batch.
  - The `batchIds` expired write-off writes one movement per ticked batch.
- **API:** 10 operations: listCategories, searchProducts, listStoreItems, createStoreItem, getStoreItem, updateStoreItem, createStockMovement, getStoreItemHistory, listStockBatches and updateStockBatch.
  - The authz policy is `policy/catalogue.go`, with a golden file.
- **Seed** (`internal/seed/catalogue.go`): 2,993 master products generated deterministically from in-repo lists, plus store items with stock and dated batches for every live seeded store, and the QA manual §6–7 fixtures.

## Evidence (agent)

| Check | Result |
|---|---|
| `TestSaleTakeOrder`, `TestPlainTakeOrder`, `TestStockInRoom`, `TestReverseRestore`, `TestAllocationProperties` (pure) | pass |
| `TestLedgerProperties` (DB): 30 seeds × 40 random sale, reversal, damage, count, restock and write-off steps | pass. Mutation-checked: it fails when the allocation is broken. |
| `TestWriterRefusals`, `TestWriterFreshnessAndNegativeStock`, `TestReconcile`, `TestSchemaTriggers` | pass |
| `TestCatalogueHTTP`: roles, 428/412, idempotency 400, 422s, 409, replay, batches, write-off, stale | pass |
| `TestCatalogueTenancyHTTP`: 8 calls × cross-tenant paths × owner, manager and cashier give 403 or 404, and nothing of the other business changes. A cashier for store 2 only gets 403 on store 1. | pass |
| `TestCatalogueSeedFixedValues`, `TestCatalogueSeed` | pass |
| `make check` | passed through check-auth: gosec (after G115 fixes with saturating `i32` helpers) and govulncheck ("no called vulnerabilities"). Stopped at check-infra because Terraform was not initialised in the worktree. |
| `make check-ci` | 226 self-tests ok |

All 7 property tests from the row are asserted after every random step:
1. on_hand equals Σ movements.
2. No batch goes below 0.
3. Σ batch ≤ max(on_hand, 0).
4. A sale takes an expired batch only when no unexpired stock remains.
5. Damage and downward counts take the earliest expiry first.
6. A reversal restores the same batches, never more than was taken.
7. A sold-out batch is listed by neither "going to waste" nor "expired on shelf".

The property tests use `math/rand/v2` (PCG, fixed seeds), because `rapid` was not available offline and no module was added.

## Contract and behaviour changes

- `StockBatch.etag` (`"vN"`) is new and required: it is the version a client sends in If-Match.
- Item `createdAt` and `updatedAt` are now UTC, as common.yaml requires.
- `trg_stores_touch` is recreated in 10501 with `WHEN (OLD.catalogue_seq IS NOT DISTINCT FROM NEW.catalogue_seq)`. Without that, every stock change would move `stores.updated_at`, and a pending store would lose its place in the admin review queue. The down migration restores W1-04's trigger.

## Deferred (per the brief)

- The review queue and duplicate detection.
- CSV/XLSX import.
- The snapshot endpoint and the `catalogue_changes` delta feed (the table is written; there is no endpoint).
- `POST {B}/products`.
- Photo presign.
- The `businesses.category_id` foreign key (W1-04's table; see the lead section).

## Not verified

- Concurrency under real parallel load. The lock order is designed, but there is no 50-goroutine test.
- `make reset` with the seed (the lead ran it after the merge; see below).
- Per-ID SC-CT tagging. The scenarios inside the slice are exercised but not tagged by ID.

## Lead, after the merge (2026-10-23)

Rebased onto main 3c6b020 cleanly, then fast-forwarded main to 5c75d3b (`54d2cd3` schema, `5c75d3b` writer, API, seed and tests). In the worktree before merging:

| Check | Result |
|---|---|
| `make gen` | ok |
| `go vet ./...` | ok |
| golangci-lint | 0 issues |
| `go test -race ./...` (whole backend, DB tests on, after the rebase) | exit 0, 44 packages ok |
| Diff scan | no secret files |

I reviewed the `trg_stores_touch` change against W1-04's 10401: it only skips the touch when nothing but `catalogue_seq` moved, and no W1-04 write changes `catalogue_seq`. Accepted.

Decisions on the agent's questions:

- **Reserve and release:** correctly left to W2-02. 03-data-model puts `stock_reservations` and the `reserved` trigger with orders. W1-05 reads `reserved` (available = on_hand − reserved) and never writes it. The W2-02 brief carries this.
- **Damage and expired write-offs capped at available, 422 with the max:** accepted.
- **`batchIds` naming batches that have not expired:** open and small, and not on the core loop. The expired write-off should refuse a batch that has not expired. Follow-up with B-07.
- **Batch PATCH accepting a past expiry:** accepted, because it corrects a typing mistake.
- **Reconcile only alerts on `batch_over`:** accepted.
- **`businesses.category_id` foreign key:** a follow-up migration in W1-04's block, after the weekend.
- **Lock order for callers:** W2-02 and W2-04 must take stores → item → batches. This is in the W2-02 brief.
