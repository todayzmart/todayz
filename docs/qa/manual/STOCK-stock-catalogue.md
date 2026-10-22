# STOCK Stock, catalogue and offline sync

Stock movements of every source and who may write them, the stock math (on hand = sum of movements; reservations are not movements), freshness and stale stock, low stock, the master and store catalogues, import, batches, stock races between online orders and the counter, and offline POS sync conflicts. Voice and bill-scan drafts are in MQ-11; this file checks their result on stock.

- **Covers:** BK-02, BK-03, BK-32, BK-36, BK-37; POS-04, POS-05, POS-06, POS-24; E2E-11; SC-CT; D-01, D-36, D-38.
- **Screens:** B-03, B-05, B-06, B-07; C-07, C-08, C-09, C-11; A-08.
- **Devices:** shop D1280 (profile B, K22; profile C, M21), a second POS device (profile E, K22 on another browser profile), customer P360 (profile A), admin D1440 (profile D, G43).
- **Time:** about 2 hours.

## Preconditions

1. `make reset`; `clock set 2026-10-22T11:00:00+05:30`.
2. Seeds `<seed:items-SL>` and `<seed:pending-product>`. The import fixture `e2e/fixtures/import-5000-37-errors.csv` exists (5,000 rows, 37 bad rows). If it does not, mark STOCK-M.17 and STOCK-M.18 **B**.
3. For every stock step, open B-07 → the item → history before and after, and write down on hand, reserved and available.

## Steps

### A. Movements and the stock math

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| STOCK-M.01 ★ | K22 opens B-07 → I3 Nandini Toned Milk (on hand 30). Inline count: 28. | On hand 28. History: one `count_correction` −2 by K22 with the time. Freshness resets ("updated 0 min ago" or "just now"). | `R-4.4` `R-4.5` `R-4.6` B-07 | ▢ | |
| STOCK-M.02 | As K22 look for Restock, Damage, price edit, batches and Import on I3. | All hidden for the cashier. Via cURL, `POST {S}/items/{I3}/movements` with `kind` `restock` → `403`; `damage` → `403`; `PATCH {S}/items/{I3}` with a new price → `403`. | `R-3.9` AU-10 D-38 D-06 D-65 B-07 | | |
| STOCK-M.03 | M21 restocks I1 Amul Butter +12 with note "QA". Records damage −1 on I5 Coca-Cola with note "dented". Marks 2 units of I8 Curd as `expired`. | I1 10 → 22 (`restock`). I5 12 → 11 (`damage`). I8 20 → 18 (`expired`). Each row shows source, quantity, actor M21 and time. | `R-4.4` B-07 | ▢ | |
| STOCK-M.04 | Via cURL as M21, post a movement with `kind` `voice` and another with `kind` `gift`. | Both → `422`: the source is not in the D-01 list. No row written. | `R-4.4` SC-CT D-01 | | |
| STOCK-M.05 ★ | C10 places a pickup order for I1 ×3. Before accepting, read I1 in B-07. K22 accepts. Read again. | Before: on hand 22, reserved 0, available 22, no new movement. After accept: on hand 22, reserved 3, available 19, still no movement; "Reserved for orders 3" shows apart from the history. | `R-4.5` `R-5.19` `R-5.20` BK-36 B-07 | ▢ | |
| STOCK-M.06 | K22 enters C10's code. Read I1. | On hand 19, reserved 0. One `sale_online` −3 movement. Sum of the history rows equals 19. | `R-5.21` BK-37 B-07 | | |
| STOCK-M.07 | Place, accept and then cancel (customer) another I1 ×2 order. Read I1. | Reserved back to 0, on hand 19, no movement for the cancelled order. | `R-5.23` BK-37 | | |

### B. Low stock and stale stock

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| STOCK-M.08 | B-07 filter Low. | I6 Dolo (1 left) is listed; items with 5 or more are not (`low_stock_default` 5). B-03 KPI "items low" counts the same items. | `R-4.7` B-03 B-07 | ▢ | |
| STOCK-M.09 | M21 counts I5 Coca-Cola to 5, then to 4. | At 5: low (quantity at or below the threshold is low, 01 §4). At 4: low. | `R-4.7` | | |
| STOCK-M.10 | M21 sets I1's own low-stock threshold to 25 in the item detail. | I1 (19) shows as low. Restore 5. | `R-4.7` | | |
| STOCK-M.11 ★ | In profile A open I7 Toor dal (last update 73 h ago) on C-07 and C-08. | "Stock not confirmed recently" instead of the freshness line. In search "toor dal", Sri Lakshmi ranks below a store with fresh stock at a similar distance (record order). | `R-4.6` `R-11.5` SC-CT C-07 C-08 | ▢ | |
| STOCK-M.12 | K22 counts I7 (8 → 8, "confirm count"). Reload C-08. | "In stock · updated 0 min ago" (or "just now"). B-07 freshness updated. | `R-4.6` B-07 C-08 | | |
| STOCK-M.13 | `clock +71h59m`. Reload C-08. `clock +2m`. Reload. | At 71 h 59 m: still the freshness line ("updated 71 h ago" or similar). At 72 h 01 m: "Stock not confirmed recently" (`stock_stale_after_hours` 72). | `R-4.6` | ▢ | |

### C. Catalogue: master, store items, missing products, import, batches

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| STOCK-M.14 | G43 adds a master product "QA Ghee 500 ml" with barcode `8900000000017` in A-08. In profile B open B-07 → Add item → search "QA Ghee". | Found in the master catalogue. No store has it in stock: not in Sri Lakshmi's B-07 list until added; C-05 search for "QA Ghee" finds nothing. | `R-4.1` `R-4.2` A-08 | ▢ | |
| STOCK-M.15 | M21 adds "QA Ghee 500 ml" to Sri Lakshmi with price ₹310, MRP ₹320, opening stock 4. | Store item created. History: `opening` +4. It shows in C-05 within a minute. | `R-4.1` `R-4.4` | | |
| STOCK-M.16 | M21 creates a new product "QA Rasam Powder 100 g", barcode of I1 (`8901262010016`), price ₹45, stock 6. | Usable at once in Sri Lakshmi (B-05 and C-07). It lands in G43's A-08 review queue with I1's master product as a duplicate suggestion (same barcode). | `R-4.3` SC-CT A-08 | ▢ | |
| STOCK-M.17 | M21 opens Import, uploads the 5,000-row fixture. | The validation preview lists all 37 bad rows with row number and reason. Nothing applied yet. | `R-4.4` SC-CT B-07 | ▢ | |
| STOCK-M.18 | Apply. | 4,963 rows applied. New stock rows show `import` movements. Record the time taken. | `R-4.4` SC-CT | | |
| STOCK-M.19 | M21 adds a batch to I3: 10 units expiring in 6 days, cost ₹20. Then one expiring in 5 days. Open Overview → Demand insights → Going to waste. | The 5-day batch shows (`insight_expiring_within_days` 5); the 6-day batch does not. I8's 3-day batch shows. | `R-12.8` SC-CT B-03 | ▢ | |

### D. Stock races with customers

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| STOCK-M.20 | C10 adds I6 Dolo ×1 (the last unit) and opens checkout (quote shown). K22 sells I6 ×1 at the POS. C10 taps Place order. | `409 insufficient_stock` naming the Dolo line. C-11 marks the line unavailable. No order created, no credits held. | `R-5.19` BK-02 C-11 `insufficient_stock` | ▢ | |
| STOCK-M.21 | M21 restocks I6 +2. C10 and C11 each place an order for I6 ×2. K22 accepts C10's. K22 accepts C11's. | First accept reserves 2 (available 0). Second → refused with `insufficient_stock` (record the code); C11's order stays `placed` for a reject or a partial accept. Reserved never exceeds on hand. | `R-4.5` BK-32 | | |
| STOCK-M.22 ★ | C10 quotes a cart with I2 ×5 (₹70). M21 changes I2's price to ₹15. C10 taps Place order. | `409 price_changed` with the new quote (₹75). C-11 shows the new total and needs a second tap. The second tap places the order at ₹75. | `R-12.4` BK-03 C-11 `price_changed` | ▢ | |

### E. Offline POS and sync conflicts

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| STOCK-M.23 | Set I6 on hand to 1 (M21 count). Profiles B and E are two POS devices at Sri Lakshmi. Put both offline (DevTools → Offline). Each sells I6 ×1, cash. | Both sales saved locally. Each shows "Offline · 1 sales waiting to sync" (record the exact plural). | `R-12.9` POS-04 B-05 | ▢ | |
| STOCK-M.24 ★ | Bring both online. Wait for sync. Open B-07 → I6. | Both sales accepted (no sale is refused). On hand −1, and I6 is flagged "count needed". History shows two `sale_pos` rows. | `R-4.5` POS-04 B-07 | ▢ | |
| STOCK-M.25 | On profile B, go offline, sell I2 ×1, then in DevTools re-send the same sync request twice (Replay XHR) after going online. | One sale on the server (same client id). | POS-05 | | |
| STOCK-M.26 | Set profile E's system clock 2 hours behind (or use DevTools sensors to override time if available; otherwise mark **B**). Go offline, sell I3 ×1, go online. Open sales history and B-12. | `sold_at` = device time (2 h earlier); `received_at` = server time. B-12 reports place the sale at the device time, clamped to no more than 72 h before receipt. | POS-06 | ▢ | |
| STOCK-M.27 | Profile E online; read `GET {S}/catalogue/changes?cursor=<cursor>` with a cursor from now. `clock +29d`. Repeat with the same cursor. `clock +2d`. Repeat. | At 29 days: a delta of changes. At 31 days: `410 resync_required`. | `R-12.9` POS-24 D-36 `resync_required` | | |
| STOCK-M.28 | Open the business app on profile E after the 31-day jump. | The app downloads the full snapshot (a progress or "Updating catalogue" state), then works on deltas from the new cursor. No stale prices on the POS. | POS-24 B-05 | ▢ | |
| STOCK-M.29 | Profile B offline. M21 (online, profile C) changes I1's price to ₹60. On profile B sell I1 ×1 at the cached ₹58. Go online and sync. | The sale is kept at the ₹58 that was charged. The POS then shows ₹60. The sale is not flagged (no risk flag in v1, D-93). | `R-12.9` POS-03 | | |

### F. A confirmed voice draft reaches the customer (E2E-11)

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| STOCK-M.30 | `ai_voice_stock_enabled` on (SA48). M21 records "Received 12 Amul butter 100 g" with the recorded fixture, edits the quantity to 10 and confirms. | One `restock` +10 movement by M21 on I1; no `voice` source. | `R-12.7` E2E-11 VO-02 B-06 | | |
| STOCK-M.31 | In profile A reload I1 on C-08. | Stock and "updated 0 min ago" reflect the confirmed change. | `R-4.6` E2E-11 C-08 | ▢ | |

## Cleanup

1. Reset system clocks on the POS devices. Bring all profiles online.
2. `restore defaults`: `ai_voice_stock_enabled` false.
3. `clock reset`; `make reset`.
