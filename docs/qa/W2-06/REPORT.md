# W2-06 Reviews and ratings: QA report

Date: 2026-10-24. Branch `claude/wp-w2-06-reviews-8e844f`. Spec: 01 §10 and §17, 03 `reviews`, 04 §2.3, §2.4, §2.6, D-41, D-59, D-60, D-92 and D-140 (`docs/plan/12-decision-log.md`). Everything ran locally; nothing touched a cloud account. Backend only: the screens belong to W2-11 (shop reply and report) and W3-05 (console).

## What was built

- **Migration `20601_w2-06_reviews.sql`** (D-89 block 20601–20699):
  - `reviews` holds stars, text (at most 1000), tags, `edited_until`, and the moderation columns (`hidden_at`, who, reason).
  - It is unique on `order_id` and on `pos_sale_id` (one review per order or sale).
  - `review_replies` has one reply per review.
  - `review_reports` is unique per reporter per review and has a status (`open | upheld | dismissed`).
- **`internal/reviews`:**
  - **Customer:** `POST` and `PATCH /v1/me/reviews`, `GET /v1/me/reviews`, `POST /v1/me/review-reports`.
  - **Public:** `GET /v1/discover/stores/{slug}/reviews` (visible reviews only, first name of the customer).
  - **Shop** (owner and manager): list, reply, edit reply, report.
  - **Admin:** list, hide and restore (review and reply), list reports, resolve a report. Support and above, otp+totp.
- **Rating:**
  - `stores.rating_avg` and `rating_count` have one writer (D-59).
  - The value is the Bayesian average of the visible reviews (prior 4.0, weight 5), computed in SQL.
  - Each write recomputes its store in the same transaction under a per-store advisory lock; the hourly `store_rating` job (`internal/jobs/reviews.go`) heals drift.
- **Authz:** `policy/reviews.go` with `testdata/reviews.golden`.
- **Moderation:** every hide, restore, reply hide, reply restore and report resolution writes an audit row in the same transaction. Hiding auto-upholds the review's open reports.

## Evidence

All commands ran from `backend/` after merging `main` (a1ddd6a) into the branch. Go tests used the shared test Postgres (`TODAYZ_TEST_PG_URL_FILE`) with `-p 2`.

| Check | Result |
|---|---|
| `gofmt -l`, `go vet ./internal/reviews/` | clean |
| `golangci-lint run` on `internal/reviews`, `internal/jobs`, `cmd` | 0 issues |
| `gosec` on `internal/reviews`, `internal/jobs`, `cmd` | 0 issues (the `int32(lim+1)` conversions carry `#nosec G115`, the repo's convention, lim is clamped) |
| `settingslint` | clean |
| `make gen` (includes `apigen check`) | ok |
| `redocly lint` (`check-contract-lint`) | valid; 10 warnings, 2 of them mine: `ReviewTagPositive` and `ReviewTagNegative` are listed vocabulary schemas no operation references |
| api-client lint, typecheck, test | 42 tests passed |
| `go test -race -p 2` on `internal/reviews`, `internal/jobs`, `internal/platform/authz/...` | ok |
| `go test -race -p 2` on `cmd/todayz` and `internal/platform/httpx/...` | ok (includes `TestRoutesAreInTheContract`, `TestOneMountPerDomain`, the problem registry) |

Earlier on the branch, before the merge, the same packages passed with a container per package, and `golangci-lint` and `gosec` were clean on the pre-fix tree except the findings listed above.

**Scenarios covered** (`internal/reviews/reviews_test.go`):

| Scenario | Tests |
|---|---|
| Eligibility: own order, `collected` or `delivered` only | `TestEligibility`, `TestPOSSaleEligibility` |
| One review per order | `TestOneReviewPerOrder` |
| Edit window (RV-01), edit rules, hidden review not editable | `TestEditWindowRV01`, `TestEditRules` |
| Reply: once, editable, shop roles | `TestReply` |
| Report flow: customer and shop, own review refused, duplicate | `TestReports` |
| Hide sets `hidden_at`, rating recalculates; restore clears it | `TestHideRestoreRecalculates` |
| Reasons required, support and above, invalid transitions | `TestModerationRules` |
| Both audited | `TestModerationIsAudited` |
| Reply hide and restore | `TestHideReply` |
| Hiding upholds open reports; resolve a report | `TestHideUpholdsOpenReports`, `TestResolveReport` |
| Rating equals the Go oracle for many shapes | `TestBayesAvg`, `TestRatingMatchesOracle` |
| Concurrent writes keep the rating exact; the job heals drift | `TestConcurrentWritesKeepTheRatingExact`, `TestRecomputeAllHealsDrift` |
| Public list shows visible only; paging; my reviews; validation | `TestPublicListVisibility`, `TestPaging`, `TestMyReviews`, `TestValidation` |

## Extras beyond doc 04 (recorded in D-140 and 04)

- `GET /v1/me/reviews`.
- `POST /v1/admin/reviews/{id}/reply/hide` and `.../reply/restore` (01 §10 says support can hide a reply).
- The admin endpoints, pulled forward from W3-05 so the moderation flow can be tested; W3-05 builds the screens.
- The service reads `orders` directly (`GetOrderForReview`) instead of calling W2-02 through an interface, because the check is one row and the orders interface does not expose it. If the merger prefers a seam, it is one query.

## Files outside the owned paths

None beyond the designated stubs: `cmd/todayz/routes_reviews.go` and `internal/jobs/reviews.go`. Docs touched: `docs/plan/12-decision-log.md` (D-140), `docs/plan/04-api.md` (three rows, owner column for the admin rows). `backend/gen`, `shared/api-client/src/gen` and the bundle are not committed.

## Questions (each with the default built)

1. **Edit window boundary.** The plan says "for 7 days". Built: allowed while `now <= created_at + days`. Say if it should be `<`.
2. **Account deletion.** What happens to a deleted customer's reviews? Built: they stay, with the first name only; no deletion hook is registered. The alternative is to anonymise or delete them.
3. **Cashiers.** They cannot see or reply to reviews (owner and manager only: 01 §3 lists review replies as manager and above).
4. **"New review" notification** (01 §10 table, in-app). Not sent. It needs the W2-05 consumer; one enqueue call in `create` when it lands.
5. **Resolving a report as `upheld`** does not hide the review. Hiding is separate; hiding auto-upholds open reports. Say if upholding should hide.
6. **Duplicate report** returns `201` with the first report rather than `409`.
7. **Reporting your own review** is `422`.
8. **Re-hide / re-restore** answers `409 invalid_transition`.

## Seams the merger must know

- **POS-sale reviews wait for W2-04.** `reviews.Deps.SaleSource` is nil in `routes_reviews.go`, so a `posSaleId` is refused (`422`) until W2-04 lands `pos_sales` and passes a `SaleSource`. The `pos_sale_id` column and unique index exist already, without a foreign key (the table is not on main); W2-04 or the merger adds the FK in a later migration.
- **Orders trigger in tests.** The orders table has `trg_orders_guard`; the reviews tests disable it in their per-test database to insert already-collected orders. No production path does.
- **Generated admin enum.** The generated resolution enum shows a `<nil>` member from a nullable enum in `admin-reviews.yaml`. Harmless; tidy in W3-05.
- **i18n.** No per-code problem messages exist in `shared/i18n` for other domains either, so none were added for `review_*`.
- **Not run here:** `make check` as a whole, the Schemathesis smoke (`contract-smoke`), e2e and load. The low-RAM rule allowed area checks only. The `cmd/todayz` route and contract tests above did run.

## Commits

`4343ce0` contract, migration, queries, policy · `006657c` service, handlers, routes, job · `a9b47d0` tests · `f0e13c5` lint fixes · the merge of `main` · docs and this report (last commit).
