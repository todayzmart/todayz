# D-270 · No item goes below zero at the counter

> As built on main at d641cbe6 (27 Oct 2026).

The founder asked for this (relayed by the lead): "no item should go negative". Before this change, the POS showed a "more than the count on hand" banner and still let any quantity into the bill. Now the bill stops at what is available, and the counter asks for a count instead.

## What changed

| Where | What |
|---|---|
| Cart (`business/src/pos/cart.ts`) | A line of a catalogue item never holds more than the item's **available** (on hand − units blocked for accepted, not yet collected or delivered online orders). Lowering a line always works. A new `fit` action sets a line after a count. `firstShortLine` replaces `shortLines` and checks against the live catalogue. |
| Update stock dialog (`UpdateStockDialog.tsx`) | Opens when a tile, a search result, +, the keyboard or a typed quantity asks for more than is available, and when Charge finds a line the catalogue has since made short. It shows on hand, blocked for online orders and available, then "Counted now". The box "This count includes the N units set aside for online orders" appears only when N > 0. Unticked (the default), on hand = count + blocked; ticked, on hand = count. A preview line follows. Save writes a normal `count_correction` through the outbox (any role, works offline) and fits the line to the new available, never beyond. Cancel changes nothing. |
| Tiles and search results | Show "N blocked for online orders" when N > 0. They show "Count needed" for an item an offline race took below 0. |
| Engine (`business/src/sync/engine.ts`) | A sale or count recorded on the device is applied to the stored catalogue at once, so offline bills see it. A refused sale gives its units back. Online `409 insufficient_stock` is handled below. |
| Server: online sale (`backend/internal/pos/sale.go`, `catalogue/writer.go`) | `POST {S}/pos/sales` refuses a catalogued line above available with `409 insufficient_stock` and writes nothing. Lines of one item in a bill are summed. The response's `extensions` carry `storeItemId` and `available`. The code already existed for online orders, so no new problem code. |
| Server: damage and expiry (`writer.go` `Removal.CapOnHand`, `pos/sync.go`) | A synced `damage` or `expired` above on hand is `rejected: invalid`. It used to be "not capped" (D-130), which D-270 supersedes. The device caps these at on hand too. |
| Server: reads (`catalogue/items.go`, `pos/sync.go`) | An item below 0 reads `onHand: 0`, `available: 0`, `countNeeded: true`. The contract has `StoreItem.onHand` and `available` at `minimum: 0` and adds `countNeeded`. |

## The race, and why sync still keeps it

Two devices offline can each sell the last packet, and both sales really happened: the goods left and the money was taken. Refusing either at sync would make the books wrong in a worse way: money with no sale. Sync therefore never refuses a sale for stock. On hand on the server can end at −1, but nothing shows a negative: the API answers 0 / 0 with `countNeeded: true`, the tile says "Count needed", and the dialog's banner asks for a count. The next count, from any device and any role, sets on hand right.

There is one online case of the same kind. The device sends a sale by itself right after the cashier has taken the money and reset the bill. If the server answers 409 because an online order or another device took the units a moment earlier, the device:

- keeps the sale pending, and sync applies it;
- adopts the server's available for that item, less its own pending sales;
- opens Update stock with "This sale went through, but TodayZ had less … than was sold".

A bill QR sale needs the server to complete, so a 409 on one is refused and its units go back to the device's stock.

No money or permission rule changed: counting was already every role's (`stock.count`, D-65), and damage stays owner and manager only. The lead was not asked anything.

## Screenshots

- `pos-stock-tile-blocked.png`: Atta tile with "3 blocked for online orders", two in the bill.
- `pos-stock-dialog-blocked.png`: the dialog with the checkbox (on hand 5, blocked 3, available 2; counted 4 → on hand 7, available 4).
- `pos-stock-dialog-plain.png`: Ghee (nothing blocked, nothing available). The dialog has no checkbox.

They were taken by `business/e2e/sell.spec.ts` ("a line never goes past available…") with `TZ_QA_DIR=docs/qa/D-270-pos-stock-cap`.

## Tests

| Test | Covers |
|---|---|
| `backend/cmd/todayz/pos_stock_cap_http_test.go` `TestPOSOnlineSaleCappedAtAvailable` | 409 above available with an online order holding a unit. Two lines of one item are summed. Nothing is written. Selling exactly available works. The held unit stays. |
| `TestPOSOfflineRaceShowsZero` | Two offline sales of the last unit both apply. On hand −1 in the table reads 0/0 with `countNeeded`. A count clears it. |
| `TestPOSSyncDamageCappedAtOnHand` | Damage and expired above on hand are rejected `invalid`; up to on hand applies. |
| `pos_http_test.go` POS-04 | Updated: an oversold item now reads 0/0 with `countNeeded`. |
| `business/src/pos/cart.test.ts` "stock cap (D-270)" | Cap on add, step and set; nothing added at 0; `fit`; `firstShortLine`. |
| `business/src/pos/Counter.test.tsx` | The tile shows blocked units. Raising past available opens the dialog and leaves the bill alone. Unticked save writes count + blocked and fits the line. Ticked save writes the count. No checkbox when nothing is blocked, and Cancel writes nothing. An online 409 opens the dialog and keeps the sale pending. |
| `business/src/sync/engine.test.ts` "stock at the counter (D-270)" | A sale lowers device stock and a rejection restores it. A count is applied locally and queued as a `count_correction` movement. A 409 keeps the sale pending, adopts the number and sets `short`. A bill QR sale is rejected. |
| `business/e2e/sell.spec.ts` | The journey above, on the production-shaped build. |

## Checks run

- Business:
  - `typecheck`, `lint` and `lint:css` are clean;
  - `test`: 55 files, 563 tests passed;
  - `e2e/sell.spec.ts`: 6 passed;
  - `size`: 179.76 kB of 250 kB JS, 12.29 kB of 20 kB CSS, on the e2e build. A production build needs the real Turnstile key, which is not on this machine.
- `@todayz/i18n lint:messages` and `gen` pass. 17 new pos keys are in en, hi, kn, te and ta. `pos.banner.short.*` is removed.
- Backend, after `make gen`: `go vet` and gofmt are clean. `go test -race` passes on `internal/pos`, `internal/catalogue`, `internal/jobs` and `cmd/todayz -run 'POS|Pos|Sync|Contract|Routes|Stock'`.
- Also fixed: `business/src/orders/sales.module.css` from D-268 used `min-width`, which `lint:css` refuses. It now uses `width >= 1024px`.

## Not done here

- `backend/internal/catalogue/demo.go` (the D-269 demo catalogue, not this change) fails `golangci-lint` goimports.
