# W2-02 delivery: report

This is the delivery half of orders that W2-02 had deferred. It covers:

- quote and create
- dispatch with its timer
- the partial fee
- delivering with the code
- failing a delivery
- confirming the return, plus the return job and sweep
- the full phone while out for delivery
- no no-show for delivery

It also covers the business B-04 card and detail. Branch `claude/eager-goldstine-6fb608`, not pushed.

## Checks run

After the review fixes (24 Oct, after `make gen`):

| Check | Result |
|---|---|
| `go test ./cmd/todayz/ -count=1` | ok, 174 tests pass, 0 fail (58 s) |
| `go test` notify, jobs, platform/settings, platform/migrate, httpx/openapi (+schemacheck) | ok |
| gofmt, `go vet`, `golangci-lint` (orders, notify, cmd/todayz), settingslint | clean, 0 issues, ok |
| business `tsc --noEmit`, `vitest run` | ok; 372 tests pass (2 new in `labels.test.ts`) |
| business `eslint src/orders`, `shared/i18n` `lint:messages` | clean; 0 errors |

First delivery hand-over:

| Check | Result |
|---|---|
| `go test ./internal/orders/... ./internal/jobs/... ./internal/notify/... ./cmd/todayz/... -count=1` | ok (cmd/todayz 93 s, jobs, notify); `internal/orders` has no test files of its own |
| `gofmt -l cmd internal` | clean |
| `go vet` (orders, jobs, notify, cmd/todayz) | clean |
| `golangci-lint run` (orders, jobs, notify, platform/notice, cmd/todayz) | 0 issues |
| `go run ./internal/devtools/settingslint .` | ok |
| business `tsc --noEmit` | ok |
| business `vitest run` | 41 files, 370 tests pass (orders: 27, 7 new) |
| business `eslint --max-warnings 0 .`, `stylelint` | clean |
| `shared/i18n` `lint:messages` | 0 errors |
| Playwright `orders.delivery.shots.spec.ts` (360 px, faked API) | 5 pass, no horizontal overflow; `orders.shots.spec.ts` still passes |

## Evidence per BK row

| Row | Test(s) | What it proves |
|---|---|---|
| BK-23 | `TestBK23DeliveryRadiusMinimumAndFee`, `TestBK23DeliveryClosed`, `TestBK23DeliveryHoursWiderThanCounter` | Outside the radius, the quote and the place both refuse. The minimum and the free-above threshold set the fee. Outside delivery hours the order is `delivery_closed` with `nextDeliveryWindow`. The feature flag gates delivery. The address is stored as a snapshot. With counter hours 09–20 and delivery hours 09–22, at 21:00 the delivery quote is orderable and the place succeeds, while a pickup place is `store_closed`. With `closed_until` set, delivery is `delivery_closed` on both the quote and the place, and pickup is `store_closed` (D-219). |
| BK-23b | `TestBK23bItemSubtotalForFeeAndBillOffer` | The free-delivery amount and a minimum-bill offer are both judged on the item subtotal (D-94). |
| BK-24 | `TestBK24DeliverWithCode`, `TestBK24CancelBeforeDispatch` (also at and 1 min after `dispatch_by`: dispatch, the customer's cancel and the manager's cancel are all `409 invalid_transition`, nothing changes, then the timer expires the order), `TestBK24PartialRecomputesFee`; vitest "dispatches an accepted delivery order…" and "…delivers with the code" | Accept starts the dispatch clock. Dispatch sends the order out. Verify marks it delivered. Cancels are allowed before dispatch. A partial accept recomputes the fee. In the UI, Dispatch replaces Mark ready, and the code entry appears only while out for delivery, with delivery wording. |
| BK-25 | `TestBK25DispatchTimeout` | Not dispatched by `dispatch_by`: the order expires `dispatch_timeout`, the stock and credits go back, and the store gets a strike. The customer's notice uses the new `dispatch_timeout` variant, not `expired`. A repeat run does nothing. |
| BK-26 | `TestBK26FailedDeliveryAndReturn`, `TestBK26FailedDeliveryReleasesCredits`, `TestBK26ReturnRemindersAndAutoConfirm`, `TestBK26ReturnSweep`, `TestBK26ReturnReminderSettings`, `TestBK26ReturnJobAfterStaffConfirm`, `TestBK26ReturnOutcomeIsStoreOnly`; vitest return cases; shots `w2-02-delivery-return-*-360.png`, `w2-02-delivery-returns-card-360.png` | A failed delivery takes a reason, and `other` needs a note. The held credits are released and no cashback is paid. `returned` works for cashier and up; `damaged` is manager and up and writes damage movements. Reminders go out at 2 h and 24 h. At 72 h the return settles `unconfirmed`, the stock is released and the risk flag is raised. With every job lost, the sweep does the same. With the settings at 3 h and 30 h, both the timers and the sweep follow them, and the 72 h auto-confirm does not move (D-218). After a staff confirm, every later step of the return job is a no-op: no reminder, no flag. The customer's timeline never shows the `delivery_failed → delivery_failed` outcome event; the store's timeline keeps it, with reason `return_damaged`. In the UI, a cashier sees "Ask a manager to record damage", the time since the failure is shown, and an unconfirmed return shows "Auto-confirmed after 72 h". |
| BK-27 | `TestBK27PhoneOnlyWhileOutForDelivery`; vitest "shows the full phone only while out for delivery…", "lists returns to confirm…"; shots `w2-02-delivery-out-card-360.png`, `w2-02-delivery-out-detail-360.png` | The full phone appears in the detail and the list (`?status=` tab) only while the order is out_for_delivery. It is masked before (accepted) and after (done, returns), and is never shown for pickup. The dispatch response never carries it, and neither does an idempotent replay of the dispatch after delivery. The UI shows it as a `tel:` link. |
| Scope | `TestDeliveryActionsNeedADeliveryOrderOfTheStore` | A pickup order is `409 invalid_transition` on dispatch, fail-delivery and confirm-return. Another store's delivery order is 404 on those three routes, both for a cashier of S2 on S2's path and for another business's owner on their own path. |
| BK-41 | `TestBK41NoNoShowForDelivery` | A delivery order still accepted 30 min past its hold gets no no-show strike and is not expired as a no-show. |

`TestOrderTransitionTable` lists the three delivery moves as covered by these tests. `TestMachineMatchesTheGuard` still passes, and now rolls back its open transaction in a `t.Cleanup` so a failed check can no longer hang it until the test timeout.

## Review fixes (lead review, 24 Oct)

1. **Quote and place agree on hours (D-219).** `closedFor` in `internal/orders/delivery.go` is the one check. A delivery order follows the delivery hours alone, and a pickup order follows the counter hours. `closed_until` applies to both. `create.go` and `quote.go` both call it. The quote calls it for delivery only, so a pickup quote stays informational (D-216) and gains no new `x-problems`. The `createOrder` text in `orders.yaml` says so.
2. **The return outcome is store-only.** The customer view keeps only real status changes (`statusChanges` in `reads.go` skips from == to). The store view is unchanged.
3. **The dispatch contract matches the code.** At or after `dispatchBy`, dispatch is `409 invalid_transition`, and the timer (or the sweep) expires the order. The code is unchanged; only the `dispatchOrder` text changed.
4. **The dispatch timeout has its own text.** The new `dispatch_timeout` order_update variant (en + hi draft) replaces `expired` in `ExpireDispatch`. The dispatch response sets `customer.phone` to null, because a stored idempotent response would replay it after delivery. The order read still gates it on out_for_delivery.
5. **i18n reason keys.** `orders.reason.accept_timeout` → `not_accepted` and `hold_timeout` → `no_show`, so the business app shows them instead of "Other reason". There is a vitest in `labels.test.ts`.

## Business UI (B-04)

- **Tabs:** "Out for delivery" and "Returns to confirm" are new. They always show, because the shop list does not say whether a store delivers.
- **Detail:**
  - The timeline for a delivery order reads accepted → out for delivery → delivered.
  - The delivery address and fee are shown, with "pays at the door".
  - Dispatch shows the dispatch-by time.
  - Delivery failed opens the reason sheet (customer unreachable, address not found, customer refused, other + note).
  - The returns panel lives in `ReturnPanel.tsx`.
- **Row:**
  - Accepted delivery orders show "Dispatch by".
  - Out-for-delivery orders show the full phone.
  - Returns show the time since the failure.
  - Settled returns show the outcome.
- **i18n:** 47 new `orders.*` keys in `shared/i18n/messages/business/orders.en.json`, English only (other locales fall back).

## Changes outside the listed scope

These were approved by the lead on 24 Oct ("add the one business notice kind…", "read the 2 h / 24 h timings from platform_settings"):

- `backend/internal/platform/notice/notice.go`: the kind `KindDeliveryReturnPending = "delivery_return_pending"`.
- `backend/internal/notify/catalogue.go` and `catalogue_test.go`: the catalogue entry (in-app only, business app), with en and hi text. The hi text is a draft, like the rest of the catalogue's Hindi.
- `backend/internal/platform/settings/keys.go`: `DeliveryReturnFirstReminderHours` and `DeliveryReturnSecondReminderHours`, added to `AllKeys`.
- `docs/plan/01-product-rules.md`: §5.3 names the two settings. §17 "Orders and strikes" has their rows. The "Fixed constants" row now covers only the 72 h auto-confirm.
- `docs/plan/12-decision-log.md`: D-218 records the change, and D-92's last line points to it.

The settings themselves are seeded by `backend/db/migrations/20204_w2-02_return_reminder_settings.sql`, which is in the W2-02 block. `delivery_return_first_reminder_hours` defaults to 2 h (1–12). `delivery_return_second_reminder_hours` defaults to 24 h (13–71). The bounds keep first < second < 72 h.
- Review fixes: `backend/internal/notify/catalogue.go` and `catalogue_test.go` (the `dispatch_timeout` variant), `docs/plan/12-decision-log.md` (D-219), `shared/i18n/messages/business/orders.en.json` (two reason keys renamed).
- `internal/orders/machine.go`: adds `codeShown`. I briefly tried making `CanMove` refuse `delivery_failed` for pickup, but the DB guard does not check fulfilment, so `TestMachineMatchesTheGuard` failed. That change is reverted. `failDelivery` now refuses a pickup order itself, so `CanMove` still matches the guard.

## Open items

1. The return reminders go to every staff member's inbox, cashiers included. Notify has no role concept: business notifications are keyed by store only, so owner-and-manager-only would need a role column or a staff join in notify. Following the lead's answer, it stays all staff for now (`TODO(lead)` in `returns.go`).
2. The customer's `delivery_failed` notice uses the generic order_update text, because the catalogue has no `delivery_failed` variant (`TODO(lead)`).
3. A partial accept recomputes the fee only when the order was free and drops below the free amount. A paid fee is kept as placed.
4. The 72 h auto-confirm and the risk flag score (50) stay constants (D-85, D-218). The UI text "Auto-confirmed after 72 h" is fixed to match.
5. There is no rider field. Dispatch has no "pick staff rider" (06-screens mentions it), and the SSE event carries no phone.
6. Damage and release are recorded per reservation line, not as a single movement per order.
7. The rider's call link shows the raw E.164 number (`+9190…`), without display formatting. The lead said that is fine for now.

## Questions for the lead

None open. Both earlier questions were answered in the review: reminders stay with all staff (open item 1), and the reason keys are renamed (review fix 5).
