# W1-04 Businesses, stores, staff, KYC: QA report

Backend domain `business` in `backend/internal/business`, mounted at `/v1/biz/businesses…` and `/v1/public/leads`. Branch `worktree-agent-a1a623e03321a8d8d`, rebased onto `main` bb315da. Everything ran locally against testcontainers databases; nothing touched a cloud account or the network. The agent could not write report files, so the lead wrote this from its hand-back.

## What was built

- **Migration `10401_w1-04_business.sql`:** `businesses`, `business_kyc`, `business_documents`, `stores` (PostGIS `geography(Point)`, hours as JSON, settings columns with CHECKs), `staff` (role, store scope, hashed and sealed invite phone) and `leads`.
- **Contract:** 21 operations under `/v1/biz/businesses` (businesses, submit, KYC, documents, staff, invite accept, stores, store submit, store settings) and `createLead` under `/v1/public/leads`. Problem codes `submit_incomplete` (409), `invalid_transition` and the KYC field reasons are registered in `api/problems/business.yaml`.
- **Onboarding:** a draft saves and resumes with `If-Match` (428 without it, 412 on a stale ETag). Submit lists every missing field in `fields` (`categoryId`, `stores`, `stores[0].addressLine1`…). A business goes draft → submitted → (admin, W1-10) verified or needs_info, whose `infoRequest` the owner sees. A store goes draft → pending_review.
- **Stores and hours:** weekly ranges plus dated holidays, in IST. A range may cross midnight; `closedUntil` closes a store by hand. `openNow` is computed on read from the injected clock.
- **Staff:** invite by phone (hashed for lookup, sealed for display, shown masked `90•••••012`), accept by the invited user only, role change, remove. A manager manages cashiers only, within their own stores; the owner row cannot be changed or removed. Accept and remove clear the invite phone.
- **KYC:** PAN and GSTIN validated (format, state code, GSTIN check digit, the PAN inside the GSTIN). PAN, GSTIN, UPI and bank account are sealed with `pii` under the field names `business.Field*`; reads show the last 4 only. The owner sees and edits; a manager sees a masked view (no legal name, IFSC or values). Every change is audited by field name, never by value.
- **Documents:** `POST` returns a 15-minute presigned PUT URL to the private bucket (`Cache-Control: no-store`), scoped to a store of the same business.
- **Store settings:** pickup, delivery, radius, fee, free-above, minimum, delivery hours, cashback (0–2000 bp, step 50), auto-accept, `asksMuted` (D-101, gated by `asks.manage`), `closedUntil`. Every change writes `store.settings.update` with before and after values.
- **Codes:** slug from the store name with a random suffix on collision; six-character short codes and referral codes from the `A-HJKMNP-Z2-9` alphabet.
- **Leads:** Turnstile first (403 on a failed check, 503 when it cannot run, fail closed), then the lead is stored with its phone sealed. The same phone, name, shop and city within 24 hours is one lead (case and spaces ignored); the answer is `202` either way. The route has no `x-idempotency`, so the site's `Idempotency-Key` is ignored and a retry with a fresh `turnstileToken` cannot hit `422 idempotency_mismatch` (D-103).
- **Authz:** the engine reads the `{bid}` and `{sid}` path scopes; `business.NewStaffResolver` supplies the caller's role and store scope (nil store IDs means every store). Policy table and golden file in `platform/authz/policy/business.go`.
- **Seed:** 24 businesses and 26 stores in Bengaluru, Hyderabad and Chennai, all fake names and fake KYC, placed by bearing and distance from the book's QA point so the radius claims hold. The identity seed's staff users are Lakshmi Traders' owner, manager and cashier (scoped to Sri Lakshmi). Without keys, only businesses and stores are seeded.
- **For W1-01b:** `business.OwnsActiveBusiness(ctx, d, userID)` (a draft counts as active). The deletion hook is wired by the lead after both merge.

## Exported for later packages

- Stores: `(*Service).Store(...)` → `StoreInfo` with `OpenAt(t)`; `business.OpenAt`, `ParseHours`, `Hours.OpenAt`, `Hours.Validate`, `HoursFromAPI`, `HoursToAPI`.
- Authz: `business.NewStaffResolver(d)` → `Membership{Role, StoreIDs}`.
- KYC and codes: `ValidPAN`, `GSTINReason`, `Slugify`; seal field names `FieldPAN`, `FieldGSTIN`, `FieldUPI`, `FieldBankAccount`, `FieldPhone`.
- Seed: `storeSeedID(key)`, `businessSeedID(key)`, `QAPoint`, `Epoch`.

## Scenario coverage

| SC | Where |
|---|---|
| SC-BZ-01 roles and the 403 matrix | `cmd/todayz/business_http_test.go` `TestBusinessHTTP` |
| SC-BZ-02 save and resume, If-Match | same |
| SC-BZ-03 submit incomplete, every missing field | same |
| SC-BZ-04 needs_info and its request | same (state set through the owner pool until W1-10) |
| SC-BZ-06, 07 open now, midnight, holidays, IST | `internal/business/units_test.go` `TestOpenAt`, `TestOpenAtEdges` |
| SC-BZ-08 invite, mask, accept, duplicates | `TestBusinessHTTP` |
| SC-BZ-09 store scoping, re-invite | same |
| SC-BZ-10–15 PAN, GSTIN, sealing, masked manager view, audit without values | same, plus `TestValidPAN`, `TestGSTINReason`, `TestLast4` |
| Leads, Turnstile, dedupe | `TestLeadsHTTP`, `internal/business/leads_test.go` |
| Seed radius claims, idempotence, sealed values, staff | `internal/seed/business_test.go` |
| SC-BZ-05, 16–18 | other WPs (W1-06 catalogue gate, W1-10 and W3 admin review) |

## Evidence

| Check | Result |
|---|---|
| `make gen` after the rebase onto bb315da | ok: 45 operations, 26 problem codes; apigen `biz 21`, `public 1` (agent) |
| `go vet`, `golangci-lint run ./...` | 0 issues (agent) |
| `gosec` (as `make check` runs it) | 0 issues after 3d624b8; gosec reads `#nosec`, not `//nolint:gosec`. The two `int32` conversions are bounded by the schema (radius 100..20000, cashback 0..2000), the validator and DB CHECKs (agent) |
| `go test -race -count=1 ./...` (with the DB) | every package ok, including `cmd/todayz` (HTTP tests, `TestContractSmoke`, `TestRoutesAreInTheContract`), `internal/business`, `internal/seed`, `authz/policy` (golden) and `settingslint` (agent) |
| `make check` in the worktree | check-repo, check-stack, check-backend, check-contract and check-mocks pass. check-frontend-types failed at `admin/src/routes/two-step.tsx(60,74) TS2339`. The lead found this is the worktree's install (shared packages not linked there); the admin typecheck passes on main with the same source. Frontend checks are re-run on main after the merge. |
| Security review | see "Review" below |

## Review

The coordinator's security review, fixed in 8d0a0ad (code and tests) and 53eaa42 (`docs/plan/03-data-model.md`). Migration 10401 was edited in place, so a local database needs `make reset`.

| Item | Outcome | Test |
|---|---|---|
| **H1** An invite reveals who holds a phone | Fixed. While a row is `invited`, `userId` and `name` are null, and `phoneMasked` is the number the inviter typed, opened from the row's own seal. The account's name and phone show only once the invite is accepted. This applies to the invite response, PATCH and ListStaff. `StaffMember` gains an optional `inviteExpiresAt`. | `TestBusinessHTTP`: a stranger with an account and a name is invited. The row is bound to the account, but the response, PATCH and list show no id and no name. |
| **M1** Documents | Fixed. `sizeBytes` is required, between 1 and 10 MB (`maxDocumentBytes`, `TODO(lead)`). `Content-Length` is signed into the presigned PUT and returned in `upload.headers`. A new document is `pending`. There are at most 5 per business per kind (`maxDocumentsPerKind`, `TODO(lead)`), with pending ones counted, under an advisory lock. | `TestBusinessHTTP`: the headers, the signed-headers list, 0 and 10 MB + 1 give 422, the sixth pan gives 409, and another kind still gives 201. `TestPresignPutSignsContentTypeAndLength`. |
| **M2** Invite expiry and account deletion | Fixed. A new `staff.invited_at` column restarts on reinvite. An invite lapses after 7 days (`inviteTTL`, `TODO(lead)`) in accept, in the invitations list and in reinvite: a lapsed account-bound row is taken up again, and a lapsed phone-only row is withdrawn and replaced. `business.OnAccountDelete(ctx, *db.DB, clock.Clock, ids.ID)` and `(*Service).OnAccountDelete` mark the account's pending invites `removed`, both those bound to the user id and those waiting for its phone hash, and clear their phone. They are idempotent and leave active rows alone. The lead does the wiring. | `TestStaffInvitesHTTP` (lapsed invites are not listed and not accepted, reinvite and accept work), `TestOnAccountDelete` |
| **M4** Re-review and closed businesses | Fixed. A real change to name, address, location or phone sends a `live` or `paused` store to `pending_review`. Saving the same values does not. The audit records `{status}` → `{status, fields}`. Store create, store update and KYC PUT take the business row `FOR SHARE` and answer 409 `invalid_transition` when it is rejected or suspended. There is no store-count cap. | `TestStoreReviewHTTP` |
| **L1** Seal AAD | Fixed. The AAD is `field:owning id`: KYC uses the business id, an invite phone the staff row id, a store phone the store id and a lead phone the lead id. The seed is updated. | `internal/seed/business_test.go`: business A's PAN does not open as business B's or under the bare field. |
| **L2** Lead dedupe | Fixed. The source is part of the hash. | `TestLeadHashNormalises` |
| **L4** KYC GET inserted a row | Fixed. A GET answers an empty `missing` view (ETag `"v1"`), and only PUT creates the row. | `TestBusinessHTTP`, which runs with the row deleted. |
| Cross-tenant | Covered. The owner of X gets 403 on Y's business, KYC (GET and PUT), staff (GET and POST), stores, store, store PATCH, settings and documents. Y's store under X's path gets 403 on GET, PATCH, submit and settings, and POST X/staff with Y's store gets 422. | `TestBusinessTenancyHTTP` |
| Scoped manager | Covered. manager2, scoped to S, gets 403 on PATCH S2, on submit S2, and on removing a cashier scoped to S2. | `TestBusinessHTTP` |
| Concurrent invites | Covered. Two simultaneous invites of one phone give one 201 and one 409, leaving one invited row. | `TestStaffInvitesHTTP` |

Deferred as agreed:
- **M3:** re-auth and a notification for payout changes belong to the payout WP. That WP must also gate payouts on `business_kyc.status = 'verified'`.
- **L3:** a scoped manager's business-level reach follows 02 §3.3.
- Caps on businesses and stores per user.
- Validation running before Turnstile on the lead form.

New `TODO(lead)` questions from the review:
1. **Invite lifetime:** 7 days, a code constant (01 has no rule). It moves to `platform_settings` if it should be configurable.
2. **Document limits:** 10 MB and 5 per kind are code constants.
3. **Pending to uploaded:** nothing moves a document from `pending` to `uploaded` yet. That needs an upload-confirm call or a bucket notification.
4. **Paused stores:** a paused store's profile change also goes back to `pending_review`. This is the conservative reading; the other is to keep it paused.
5. **Deleted accounts:** their manager and cashier memberships stay `active` and point at the anonymised user. Should `OnAccountDelete` remove them too?
6. **Documents of closed businesses:** a rejected or suspended business can still create document upload URLs. The review did not list it, so it is not gated.

Wiring for the lead (in `mountBusiness`): `d.Identity.AddDeletionHook(svc)`, since `*business.Service` satisfies `identity.OnAccountDelete`. Alternatively, use `identity.OnDeleteFunc(func(ctx context.Context, uid ids.ID) error { return business.OnAccountDelete(ctx, d.DB, d.Clock, uid) })`.

Review evidence (agent, on 8d0a0ad):

| Check | Result |
|---|---|
| `gofmt -l`, `go vet ./...` | clean |
| `golangci-lint run ./...` | 0 issues |
| `gosec -quiet -exclude-generated -exclude-dir=testdata -exclude=G104 ./...` | 0 issues |
| `go run ./internal/devtools/settingslint .` | ok |
| `go test -race -count=1 ./...` (colima, GOFLAGS=-p=3) | every package ok, including `cmd/todayz` (`TestContractSmoke`, `TestRoutesAreInTheContract`, the four business HTTP tests), `internal/business` and `internal/seed` |
| `make gen` | ok after the contract change |

## Files outside W1-04's paths

- `internal/platform/authz/engine.go`, `strict.go`, `authz_test.go`, `httpx/mw/authz.go`, `httpx/mw/authn_test.go` (the `{bid}`/`{sid}` path scope)
- `cmd/todayz/security.go` (the engine gets `business.NewStaffResolver`)
- `internal/identity/human.go` (`HumanVerifier()` for the lead form)
- `docs/plan/03-data-model.md` (document `size_bytes` and `pending`, staff `invited_at`; review)

## Questions (each is a `TODO(lead)` in the code; the conservative reading is what is built)

1. **Holiday after a late night:** a range from the day before that runs past midnight stays open into a closed holiday (`hours.go`). The other reading closes the store at midnight.
2. **Verified businesses are read-only to the owner:** only a draft or needs_info business may be edited; renaming a verified business would need a re-review flow.
3. **A store may be submitted before its business is verified;** 01 §3 does not order them.
4. **A manager limited to some stores may not add a store;** 02 §3.3 does not say.
5. **Ownership transfer and leaving a business** are not in 04; the owner row is refused with 403, and nobody removes themselves.
6. **A GSTIN needs a PAN,** sent in the same request or already stored.
7. **The category is not checked** against a catalogue until W1-05 lands `categories`.
8. **`OwnsActiveBusiness` counts a draft as active;** narrow it if deletion should close a draft instead.
9. **The lead dedupe window (24 h)** is a code constant; 01 does not list it as a setting.
10. **Presigning** lives in `business/presign.go`; it moves to a platform storage package when W1-05 needs product photos.
11. **Validator before authz:** a malformed body gets 422 before a 403 (the platform's pipeline order).
12. **A removed invite is not reused:** re-inviting a phone after removing an unaccepted invite makes a new row. A removed member with an account is reused.
13. **Seed people:** the book's owners and staff (O20, M21, K22, R23, M24, X25, O26–O29) are not in the identity seed, whose staff users (N011–N013) the book lists as customers C11–C13. Only Lakshmi Traders has staff.
14. **Seed dates:** the book's absolute dates are seeded as durations before the seed epoch (Monday 2026-01-05 10:00 IST). No `category_id` or `onboarded_by` is seeded, and only Sri Lakshmi's stores have a phone.

## Not verified

- Real Turnstile, and a real R2 or MinIO upload: the verifier is faked in tests, and the presigned URL is checked against the AWS SigV4 test vector, not uploaded to.
- That MinIO or R2 refuses a PUT whose Content-Length differs from the signed one (it is signed; no upload was made).
- The seed through `make reset` on the local stack (run by the lead after the merge).
