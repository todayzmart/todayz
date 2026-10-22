# Patterns by surface

These rules come from studying the apps people in India already use daily for local shopping (Blinkit, Zepto, Swiggy Instamart, Gojek Mart), shipped screens on Mobbin from the best commerce and back-office products (Square POS and Dashboard, Shopify admin, Stripe, Wise, Instacart, IKEA, Uber Eats, Deel, HubSpot, Plain) and professional dashboard work on Dribbble. TodayZ borrows their speed and fixes the one thing they hide: which real shop has it, how far away, and how fresh the stock count is.

## Customer app (phone)

**Home.** Top bar shows the delivery or pickup location ("Jayanagar 4th Block, 560011") with a chevron to change it, then the search field, then the radius chips (1, 2, 5, 10 km), then "Shops near you" and "Popular nearby". The search field is the hero; it sits within the first 160px of the screen.

**Search results.** Results are products, grouped by shop. Each group opens with a shop header in Instacart's pattern: name, then one scannable line "Pickup in 15 min · 0.6 km · Open till 10 pm", then a freshness chip "Stock updated 4 min ago". Each product card shows image, name, unit, price, MRP strike-through and discount, a stock dot with freshness, and the shop and distance. The Add button turns into a stepper in place. A filter row of chips (Open now, Pickup, Delivery, Offers) scrolls horizontally.

**One shop per cart.** Adding from a second shop opens a sheet: "Start a new cart from Sharma General Store? Your cart from Sri Lakshmi will be cleared." The cart bar always names the shop.

**Cart bar.** Floats above the tab bar once something is in the cart: count, total, shop name, "View cart". It is the only `brand` fill on the screen while visible.

**Checkout.** Pickup or Delivery segmented control (only the options the shop offers), slot or "As soon as ready", pay at pickup or UPI to the shop, points to redeem. Login by phone OTP happens here, keeping the cart; never ask to sign in earlier.

**Order tracking.** A vertical timeline (OrderTimeline) with every status the shop sets, each time-stamped. When Ready, the screen turns to the pickup code on a black card with the shop's address and a Directions button.

**Per-shop availability.** On a product page, list every nearby shop that has it, IKEA-style: shop, distance, price, a stock dot with a word and the freshness. The shop you came from is outlined.

**Pickup code.** Four large digits on a black card, plus the shop, the address and Directions; the timeline sits below (Uber Eats PIN and Honest Greens ticket patterns).

**Wallet and referrals.** A big points balance, what it is worth in rupees, a progress track to the next reward, and a referral code with Copy and Share.

**Tab bar.** Home, Search, Orders, Wallet, Account. Inbox lives in Account and as a bell in the home top bar.

## Business app (phone and desktop)

**Overview first.** A greeting header with one line of context under it ("Thursday, 22 October · Sri Lakshmi Provision Stores · 2 orders waiting"), a date-range control and "vs last Thursday". Today's numbers (sales, orders, items sold, low-stock count) in one KPI strip with sales as the wider lead cell, then an action list before any chart (Shopify's "Fulfil orders 4" pattern): "3 orders waiting", "5 items running low", "Stock not updated in 3 days". Every action card links to the screen that resolves it.

**Orders.** A board on desktop (New, Preparing, Ready, Out for delivery, Done) and a filtered list with tabs on phone. Column headers carry the status dot, the name and a count ("Preparing 5"); empty columns can be folded into a side list. A board card has three lines: order number and customer, then items and time placed in `ink-2`, then a row of small outlined chips (Pickup or Delivery, ₹ total, slot time, "Paid UPI"), the Kargul Studio kanban pattern. New orders ring and show a toast. Each new order card shows a countdown ("Accept within 4:12") and two half-width buttons, Reject (secondary) and Accept (brand), as in Instacart's merchant approvals. Verifying pickup is a big four-box OTP input.

**Sell (POS).** Desktop: product grid on the left with search and category chips, cart on the right with customer lookup by phone, discount, payment method (Cash, UPI, Card at counter, Credit) and a black total bar with the green Charge button. Phone: search-first list with a sticky total bar; the cart opens as a sheet. Every sale updates stock instantly.

**Stock.** Table on desktop, list on phone. Each row: name, price, stock count with stepper, last updated, source of last change (POS, online order, voice, manual). Quantity edits happen in the row with an Add | Set switch, a number field and Save (Uvodo, Shopify inventory). On-hand, reserved for orders, and available are separate right-aligned columns. On desktop the table edits like a sheet: click a price or count, type, press Enter. Column headers show a small type icon. A cell that fails to save keeps the typed value with a `danger` outline and icon, and a toast says "Couldn't save the price for Amul Taaza Milk. Check your connection." with Retry. Voice update is a floating mic button: speak, see the parsed change, confirm.

**Saved and offline.** Shops lose signal, so the top bar always shows the sync state ("✓ Synced 6:40 pm", or "Offline · 3 changes waiting" in `warning`). When offline, a slim `warning-soft` banner sits under the top bar: "You're offline. Sales and stock changes are saved on this device and sync when you're back." with Retry now. Nothing is blocked; sales keep working.

**Detail drawers.** Opening an order, product or customer slides a drawer in from the right with a property list (`tz-props`): small icon and label on the left, value on the right (Status, Customer, Pickup slot, Payment, Placed, Items), then the item list, then the activity and notes. The list behind stays in place.

**Notifications inbox.** Grouped under Today, Yesterday and Earlier, each row with an unread dot, who or what, the order or product it is about and a relative time; tabs All, Orders, Stock, Reviews; "Mark all as read". Selecting a row shows its detail on the right on desktop.

**Command menu.** ⌘K (or the search field) opens one menu: categories on the left with counts (Products, Orders, Customers, Actions), results on the right, keyboard hints in the footer. "New sale", "Add product" and "Update stock" are actions in it.

**Navigation.** Phone tabs: Overview, Orders, Sell, Stock, More. Desktop sidebar adds Customers, Offers, Analytics, Wallet and referrals, Stores and staff, Help, Settings.

## Admin console (desktop)

- The same white sidebar as the business app with an "Admin" tag, dense tables with `control-sm` filters, a detail drawer from the right rather than full-page navigation, so admins keep their place in long lists.
- Verification is a review queue (Deel and Reddit mod-queue patterns): tabs Awaiting review, Needs info, Approved, Rejected with counts; bulk checkboxes; the drawer has previous and next arrows, a document checklist with a pill per document (Verified, Checking, Missing) and Approve, Ask for info, Reject, with an Undo toast.
- Above people tables (customers, admin users) a stat strip with counts that filter the table on click (HubSpot): Active, Invited, Suspended.
- Support tickets are grouped in buckets with counts: New, Waiting on us, Waiting on customer, Resolved (Plain).
- Filters are chips that show their value inline ("Status: Pending"), "All filters" opens a right drawer, and applied chips have a clear ×.
- Destructive actions (revoke an admin, suspend a business) ask for the target's name to be typed.
- Every grant of points, approval or suspension shows who did it and when in an activity log.
- Integration settings (SMS, WhatsApp, push) show a clear Off state with a `warning` banner until a key is saved and tested.

## States every screen needs

- **Loading:** skeleton rows on `surface-subtle`, never a full-screen spinner.
- **Empty:** a one-line headline in `headline`, one sentence of help and one action ("No orders yet. Share your shop link to get the first one." + Share link).
- **Error:** what went wrong and what to do, inline near the cause. Offline: a slim `warning` banner, actions queue and sync.
- **Stale stock:** availability older than 24 hours shows "Stock may have changed" in `warning` instead of a green dot.

## Dashboard craft (business and admin)

- White, grey and black, with green as a small accent. The best dashboards (Kargul Studio, Stripe, Linear) are nearly monochrome: primary buttons are black, and colour goes on status dots, the chart's highlighted bar and rises. Charts use `brand` for the line and the current bar, light grey for the other bars, and a dotted `ink-3` line for the comparison period.
- Cards: white `surface` on the white page with a 1px `line` border and no shadow, `radius-lg` (12px), 20px padding. Header row with a 15px title, an optional 13px `ink-2` description, and an info or "View" link; a dashed separator above the footer, with "View report" on the left and "Updated 6:40 pm" on the right.
- Charts: hairline `line` gridlines, three to five y ticks, area fill at 8% or less, the period still in progress drawn dashed, a crosshair with a black (`inverse`) tooltip on hover.
- Tables: 14px text, 44px rows, a 36px sentence-case header on `surface-subtle`, numbers right-aligned with tabular figures, a checkbox column first and row actions last, a totals row where sums matter, and "Showing 1–25 of 128" with pagination in the footer.
- Date range: presets on the left (Today, Yesterday, Last 7 days, Last 30 days, This month, Custom), a two-month calendar, Cancel and Apply, and always a "Compare to" control.
- Both sidebars are white `surface` with a hairline edge. 32px items, 16px icons, square count badges; the active item is a mint `wash-strong` fill with `ink` text and a `brand` icon. The admin console adds an "Admin" tag by the logo.
- Filters are 32px buttons that show their value, the label in `ink-2` and the value in `ink`: "Range: Last 30 days ▾", "Staff: All ▾" (Kargul Studio's UniOps and Sales CRM). An unset filter has a dashed border; a set one is solid.
- Inside table rows, small seven-day bar sparklines show the trend without a chart: sold per day on Stock and Top items, visits per day on Customers. Every bar is light grey and today's is solid `ink`, so tables stay calm.
- A table footer bar adds up what is in view: "86 items in view · Stock value ₹4,12,300 · Low stock 9".
- New shops see a setup checklist on Overview with a segmented progress bar ("2 of 4") that collapses once done, instead of empty charts.
- Rows can carry a thin progress bar with "12 / 20" next to it (an order being packed, a catalogue being filled), and dates that are late turn `danger` with an icon ("Oct 10 · Overdue").
- On a chart, label the peak and the dip in small dark tooltips ("Busiest: 6 pm", "Quietest: 3 pm") instead of making people hunt.
- Toolbars read left to right: search, then value filters, then sort ("Sort: Last activity ▾") pushed right, then the primary action.
- New accounts see setup cards ("Add your first 20 products", "Set opening hours", "Share your shop link") instead of empty charts.
- No dark mode: all three apps stay white, whatever the device setting.

## Reference screens

Designers on X: Marcel Kargul / Kargul Studio, https://kargul.studio/work (live Sales CRM https://sales-crm-kargulstudio.vercel.app, open source at github.com/kargulstudio/sales-crm; Kanban https://kanban-blue-beta.vercel.app; UniOps https://x.com/marcelkargul/status/2110467790343737666; Salesline data https://x.com/marcelkargul/status/2102766378016997575; projects table with offline banner https://x.com/marcelkargul/status/2112639920271667214; task drawer and inbox https://x.com/marcelkargul/status/2108671403981496458; Daymark calendar with "Saved on this device" https://x.com/marcelkargul/status/2111515571179122856; TxtCart e-commerce dashboard https://kargul.studio/work/txtcart), @sujon_co (green finance dashboard https://x.com/sujon_co/status/1956581115977757010), @barlydesign (https://x.com/barlydesign/status/1887321958880321746), @IamMuhammadMuiz (StockFlow inventory https://x.com/IamMuhammadMuiz/status/2107685598160884009).

Real shipped screens on Mobbin that this system borrows from:

- Wise web, closest palette: https://mobbin.com/screens/d9223593-f3c2-4a80-819e-959b17c68b82
- Square POS counter: https://mobbin.com/screens/8acf34b5-ee7f-4224-91c8-e4425e728680
- Square dashboard home: https://mobbin.com/screens/fa86340e-327a-45ac-8f0e-ea1cda7d7674
- Shopify admin home with action chips: https://mobbin.com/screens/a14400bb-6617-4890-af09-01c012c69998
- Shopify date-range picker: https://mobbin.com/screens/dcba8f32-4ca1-4d22-b246-4c1628026bff
- Stripe overview widgets: https://mobbin.com/screens/673e89af-fe9e-4ab9-ac27-435dedf21888
- Instacart store list: https://mobbin.com/screens/cf87da68-b55c-4234-9196-d6f98d924c6f
- IKEA per-store stock: https://mobbin.com/screens/688e3720-30f2-4d65-b6d5-685d0041d031
- Blinkit home: https://mobbin.com/screens/17db8446-ee0e-4adf-9b93-af040d77348f
- Uber Eats delivery PIN: https://mobbin.com/screens/c5cc302e-4469-4801-ae13-675188610d74
- Deel approval queue: https://mobbin.com/screens/adf2f701-8ec4-4bad-9243-1673a86dba46
- Reddit mod queue: https://mobbin.com/screens/9e81dbcf-db79-45ef-8347-08e81e1b5b77
