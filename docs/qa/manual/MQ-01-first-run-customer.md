# MQ-01 ★ First run customer

A new shopper opens the customer app for the first time, picks a language, refuses GPS, sets a PIN code and browses nearby shops and products without signing in.

- **Covers:** 08 §5 MQ-01 (language, location denied → PIN, home map, radius chips, categories, shop, product).
- **Screens:** C-01, C-02, C-03, C-04, C-05, C-06, C-07, C-08, C-21 (absent), C-24 (not here).
- **Devices:** P360 (Android Chrome) first, then the whole script again on P390 (iOS Safari). One results row per device.
- **Time:** about 25 minutes per device.

## Preconditions

1. `make reset` and `make dev` done; the health table is green.
2. Server clock reset (`clock reset`). Run between 10:00 and 20:00 IST, or `clock set <today>T11:00:00+05:30`.
3. A fresh browser profile: no site data for `app.todayz.localhost` (DevTools → Application → Clear site data).
4. Browser language set to English (India).
5. Location permission for the site is "Ask" (not pre-granted).

## Steps

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-01.01 ★ | Open https://app.todayz.localhost. | C-01 language picker shows five options, each in its own script: English, हिन्दी, ಕನ್ನಡ, తెలుగు, தமிழ். No sign-in prompt. Page background white, mint wash at the top. | `R-1.2` C-01 | ▢ | |
| MQ-01.02 | Note the selected default. | English is preselected (browser language is English). A skip action is visible. | `R-1.2` C-01 | | |
| MQ-01.03 | Tap Skip. | C-02 location screen opens. The app language stays English. | `R-1.2` C-01 C-02 | | |
| MQ-01.04 | Go back to C-01 (browser back), pick ಕನ್ನಡ, then continue. | C-02 text is now in Kannada. Kannada glyphs render in the Anek font family, not a fallback serif. | `R-1.2` C-01 G3 | ▢ | |
| MQ-01.05 | Reload the page. | The app opens in Kannada again: the choice is saved. | `R-1.2` C-01 | | |
| MQ-01.06 | Switch back to English from the language control on C-02 (or Account later), then continue. | C-02 shows three options: use current location, PIN code, search locality. The current-location option explains why location is needed before the browser prompt appears. | `R-11.1` C-02 | ▢ | |
| MQ-01.07 ★ | Tap "use current location". When the browser asks, choose Block. | No dead end: the app moves straight to PIN code entry with a short line saying location was not shared. No error toast with a raw code. | `R-11.1` C-02 | ▢ | |
| MQ-01.08 | Type PIN `56003` (5 digits) and submit. | Submit stays disabled or an inline message asks for 6 digits. Nothing is saved. | `R-11.1` C-02 | | |
| MQ-01.09 | Type PIN `999999` and submit. | Inline message that the PIN code was not found. Still on C-02. | `R-11.1` C-02 | | |
| MQ-01.10 ★ | Type PIN `560038` and pick the suggestion "Indiranagar". | C-03 Home opens. The location label at the top reads "Indiranagar, 560038 ▾". | `R-11.1` C-02 C-03 | ▢ | |
| MQ-01.11 ★ | Look at the StoreMap card. | Map shows a "you" dot, a dashed radius ring and shop pins. Radius chips 1, 2, 5, 10, 20 km with **2 km** selected (default). Stats: shops nearby **4**, open now **4**. | `R-11.1` `R-11.2` C-03 | ▢ | |
| MQ-01.12 | Read the products stat. | Products shows `<seed:products-in-radius>`: the number of in-stock listings (quantity above 0) in live stores within 2 km, not distinct catalogue products (D-93). Compare it with the seed's count. | `R-11.2` C-03 | | |
| MQ-01.13 ★ | Tap the **1 km** chip. Time it with the DevTools Performance panel or a screen recording. | Map ring shrinks; stats change to shops nearby **2**, open now **2**; the "Shops near you" list shows Sri Lakshmi and Nandi Hardware only. Update lands within 500 ms. | `R-11.1` C-03 | ▢ | |
| MQ-01.14 | Tap **5 km**. | Shops nearby **6**. Kaveri Fresh (3.5 km) and Auto Mart (4.2 km) join the list. | `R-11.1` C-03 | | |
| MQ-01.15 | Tap **20 km**, then **2 km** again. | 20 km: shops nearby **6** (Hyderabad store absent). Back at 2 km: **4**. | `R-11.1` C-03 | | |
| MQ-01.16 | Read the "Shops near you" list at 2 km. | Four StoreCards, nearest first: Sri Lakshmi 0.6 km, Nandi Hardware 0.9 km, Annapurna Provisions 1.2 km, Sri Lakshmi 2 1.8 km. None of the paused, draft, pending-review, suspended or unverified shops (PS, DS, PR, SS, NI, SB) appear. | `R-3.4` `R-11.4` SR-01 D-40 C-03 | ▢ | |
| MQ-01.17 | Read the Sri Lakshmi StoreCard. | Tinted letter avatar "S", name, "0.6 km", open state, rating in the form "4.4 · 23 reviews" style, a **"5% back"** tag (cashback 500 bp) and a **"Delivery"** tag. | `R-7.7` `R-10.4` `R-5.32` C-03 | ▢ | |
| MQ-01.18 | Read the Nandi Hardware StoreCard. | An **"In-store only"** tag. No "Delivery" tag. No "% back" tag (cashback 0). | `R-3.5` SR-05 D-41 C-03 | ▢ | |
| MQ-01.19 | Read the Annapurna Provisions StoreCard. | No "% back" tag (cashback 0) and no "Delivery" tag (pickup only). | `R-7.7` `R-5.32` C-03 | | |
| MQ-01.20 | Scroll to "Offers near you". | A carousel with the Sri Lakshmi banner "Fresh stock every morning" and the I13 offer "₹21 off". At most one item per shop on screen. | `R-12.5` `R-12.6` C-03 | ▢ | |
| MQ-01.21 | `clock set <today>T21:30:00+05:30`, then pull to refresh Home. | Open now drops to **3**: Annapurna Provisions (closes 21:00) shows a closed pin (grey) and "Closed" on its StoreCard. Shops nearby stays **4**. | `R-11.2` C-03 | ▢ | |
| MQ-01.22 | `clock set <today>T11:00:00+05:30` and refresh. | Open now back to **4**. | `R-2.3` C-03 | | |
| MQ-01.23 | Tap the map to open full screen. | C-04 opens: pan and zoom work; list/map toggle visible. | `R-11.2` C-04 | ▢ | |
| MQ-01.24 | Tap the Sri Lakshmi pin. | A bottom sheet with the same StoreCard as on Home. | C-04 | ▢ | |
| MQ-01.25 | Pan the map about 3 km east. | A "Search this area" button appears. Tapping it reloads the pins for the new centre. | C-04 | | |
| MQ-01.26 | Pan to an empty area outside Bengaluru (for example 13.30, 77.10) and tap "Search this area". Then return to Home with that area as location (use PIN `562123`). | Home empty state: "No shops on TodayZ here yet" and the action "Tell a shop about TodayZ" (opens the share sheet). | `R-11.7` C-03 G1 | ▢ | |
| MQ-01.27 | Set location back to PIN `560038`. | Label "Indiranagar, 560038 ▾"; 4 shops at 2 km. | `R-11.1` C-02 | | |
| MQ-01.28 ★ | Tap a category tile, for example "Dairy". | C-06 shows shops and products in Dairy within 2 km: Amul Butter 100 g, Nandini Toned Milk 500 ml and Curd 400 g at Sri Lakshmi. No product from KF or AM (outside 2 km). | `R-11.4` C-06 | ▢ | |
| MQ-01.29 | Tap the search field and type `amull buttr`. | C-05 suggestions show "Amul Butter" while typing. | `R-11.3` C-05 | ▢ | |
| MQ-01.30 | Submit the search. | Results grouped by shop; Sri Lakshmi header shows distance, open state and rating; ProductCard shows ₹58 with MRP ₹60 struck through and a stock tag like "In stock · updated 12 min ago". | `R-11.3` `R-4.6` C-05 | ▢ | |
| MQ-01.31 ★ | Open Sri Lakshmi from the result. | C-07 shop page: banner carousel above the header with "Fresh stock every morning"; header has name, category, 0.6 km, "Open · 8:00 am – 10:00 pm" (today's hours), rating and count, "5% back", delivery terms (fee ₹30, free delivery on ₹500+, minimum ₹150), call and directions. | `R-12.6` `R-5.29` C-07 | ▢ | |
| MQ-01.32 | Scroll the shop page. | An "Offers" row with the I13 "₹21 off" offer; in-shop search; category tabs; items with stock and freshness; a reviews tab. | `R-12.5` C-07 | | |
| MQ-01.33 | Find Toor dal 1 kg (I7, last updated 73 h ago). | Its stock tag reads "Stock not confirmed recently" (`stock_stale_after_hours` = 72). | `R-4.6` C-07 | ▢ | |
| MQ-01.34 | Find Dolo 650 strip (I6, on hand 1). | Shown as in stock with freshness; no stock number above what the shop has. | `R-4.5` C-07 | | |
| MQ-01.35 ★ | Tap Amul Butter 100 g. | C-08: image, name, pack 100 g, price ₹58, MRP ₹60, stock + freshness, shop name, a quantity stepper and Add to cart. "Other nearby shops" lists any other shop with the same product, with price and distance. | `R-4.1` C-08 | ▢ | |
| MQ-01.36 | Tap Add to cart, then +1. | CartBar appears at the bottom: 2 items, ₹116. No sign-in prompt. | `R-2.1` C-08 C-09 | ▢ | |
| MQ-01.37 | Open Nandi Hardware from Home. | C-07 shows an "In-store only" tag in the header; items browsable; Add to cart is replaced by directions and call. | `R-3.5` SR-05 BK-43 C-07 | ▢ | |
| MQ-01.38 | Open https://app.todayz.localhost/s/sri-lakshmi?r=<seed short code>&s=<seed:standee-SL> in a new tab. | The Sri Lakshmi shop page opens (QR standee deep link). No error. | `R-3.12` ST-02 C-07 | | |
| MQ-01.39 | Open the bottom nav items Home, Search, Orders, Wallet, Account in turn without signing in. | Home and Search work. Orders, Wallet and Account show a sign-in invitation, not an error. No restricted-state banner (C-21) anywhere. | `R-2.1` C-13 C-15 C-18 | ▢ | |
| MQ-01.40 | Switch the device to airplane mode (DevTools → Network → Offline) and reload Home. | Offline banner; cached shops shown and marked "may be out of date". | `R-1.1` C-03 G1 | ▢ | |

## Cleanup

1. Network back online.
2. `clock reset`.
3. Clear site data for `app.todayz.localhost` so the next script starts fresh.
