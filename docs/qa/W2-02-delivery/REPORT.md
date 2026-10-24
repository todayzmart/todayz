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
| BK-23 | `TestBK23DeliveryRadiusMinimumAndFee`, `TestBK23DeliveryClosed` | Outside the radius, the quote and the place both refuse. The minimum and the free-above threshold set the fee. Outside delivery hours the order is `delivery_closed` with `nextDeliveryWindow`. The feature flag gates delivery. The address is stored as a snapshot. |
| BK-23b | `TestBK23bItemSubtotalForFeeAndBillOffer` | The free-delivery amount and a minimum-bill offer are both judged on the item subtotal (D-94). |
| BK-24 | `TestBK24DeliverWithCode`, `TestBK24CancelBeforeDispatch`, `TestBK24PartialRecomputesFee`; vitest "dispatches an accepted delivery order…" and "…delivers with the code" | Accept starts the dispatch clock. Dispatch sends the order out. Verify marks it delivered. Cancels are allowed before dispatch. A partial accept recomputes the fee. In the UI, Dispatch replaces Mark ready, and the code entry appears only while out for delivery, with delivery wording. |
| BK-25 | `TestBK25DispatchTimeout` | Not dispatched by `dispatch_by`: the order expires `dispatch_timeout`, the stock and credits go back, and the store gets a strike. A repeat run does nothing. |
| BK-26 | `TestBK26FailedDeliveryAndReturn`, `TestBK26FailedDeliveryReleasesCredits`, `TestBK26ReturnRemindersAndAutoConfirm`, `TestBK26ReturnSweep`, `TestBK26ReturnReminderSettings`; vitest return cases; shots `w2-02-delivery-return-*-360.png`, `w2-02-delivery-returns-card-360.png` | A failed delivery takes a reason, and `other` needs a note. The held credits are released and no cashback is paid. `returned` works for cashier and up; `damaged` is manager and up and writes damage movements. Reminders go out at 2 h and 24 h. At 72 h the return settles `unconfirmed`, the stock is released and the risk flag is raised. With every job lost, the sweep does the same. With the settings at 3 h and 30 h, both the timers and the sweep follow them, and the 72 h auto-confirm does not move (D-218). In the UI, a cashier sees "Ask a manager to record damage", the time since the failure is shown, and an unconfirmed return shows "Auto-confirmed after 72 h". |
| BK-27 | `TestBK27PhoneOnlyWhileOutForDelivery`; vitest "shows the full phone only while out for delivery…", "lists returns to confirm…"; shots `w2-02-delivery-out-card-360.png`, `w2-02-delivery-out-detail-360.png` | The full phone appears in the detail and the list (`?status=` tab) only while the order is out_for_delivery. It is masked before (accepted) and after (done, returns), and is never shown for pickup. The UI shows it as a `tel:` link. |
| BK-41 | `TestBK41NoNoShowForDelivery` | A delivery order still accepted 30 min past its hold gets no no-show strike and is not expired as a no-show. |

`TestOrderTransitionTable` lists the three delivery moves as covered by these tests. `TestMachineMatchesTheGuard` still passes.

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
- `internal/orders/machine.go`: adds `codeShown`. I briefly tried making `CanMove` refuse `delivery_failed` for pickup, but the DB guard does not check fulfilment, so `TestMachineMatchesTheGuard` failed. That change is reverted. `failDelivery` now refuses a pickup order itself, so `CanMove` still matches the guard.

## Open items

1. The return reminders go to every staff member's inbox, cashiers included, because the business inbox has no role filter (`TODO(lead)` in `returns.go`).
2. The customer's `delivery_failed` notice uses the generic order_update text, because the catalogue has no `delivery_failed` variant (`TODO(lead)`).
3. A partial accept recomputes the fee only when the order was free and drops below the free amount. A paid fee is kept as placed.
4. The 72 h auto-confirm and the risk flag score (50) stay constants (D-85, D-218). The UI text "Auto-confirmed after 72 h" is fixed to match.
5. There is no rider field. Dispatch has no "pick staff rider" (06-screens mentions it), and the SSE event carries no phone.
6. Damage and release are recorded per reservation line, not as a single movement per order.
7. The rider's call link shows the raw E.164 number (`+9190…`), without display formatting.

## Questions for the lead

1. Should return reminders go only to managers and owners? That needs a role filter on the business inbox, which notify owns.
2. The existing i18n has `orders.reason.accept_timeout` and `hold_timeout`, but the backend records `not_accepted` and `no_show`. Those rows fall back to "Other reason". This predates this change and I did not touch it.

## Found in passing

`TestMachineMatchesTheGuard` hangs until the 10 min test timeout when one of its `require` checks fails. The failed check leaves the owner transaction open, so the database drop at cleanup waits on it. That caused an earlier 600 s timeout in this session. A `t.Cleanup` rollback would fix it.
