import os, sys
ROOT = sys.argv[1]
P = {
 'search':'<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>',
 'pin':'<path d="M12 21s-7-6.2-7-11a7 7 0 0 1 14 0c0 4.8-7 11-7 11z"/><circle cx="12" cy="10" r="2.5"/>',
 'plus':'<path d="M12 5v14M5 12h14"/>','minus':'<path d="M5 12h14"/>',
 'home':'<path d="M3 10.5 12 3l9 7.5V20a1 1 0 0 1-1 1h-5v-6h-6v6H4a1 1 0 0 1-1-1z"/>',
 'bag':'<path d="M5 8h14l-1 12H6z"/><path d="M9 8V6a3 3 0 0 1 6 0v2"/>',
 'receipt':'<path d="M6 3h12v18l-3-2-3 2-3-2-3 2z"/><path d="M9 8h6M9 12h6"/>',
 'wallet':'<rect x="3" y="6" width="18" height="13" rx="2"/><path d="M3 10h18M16 14.5h2"/>',
 'user':'<circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/>',
 'clock':'<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
 'check':'<path d="m5 12 5 5L20 7"/>',
 'store':'<path d="M4 9 5.5 4h13L20 9"/><path d="M4 9h16v11H4z"/><path d="M10 20v-6h4v6"/>',
 'truck':'<path d="M3 6h11v10H3z"/><path d="M14 10h4l3 3v3h-7"/><circle cx="7" cy="18" r="2"/><circle cx="17" cy="18" r="2"/>',
 'mic':'<rect x="9" y="3" width="6" height="11" rx="3"/><path d="M5 11a7 7 0 0 0 14 0M12 18v3"/>',
 'chev':'<path d="m9 6 6 6-6 6"/>','down':'<path d="m6 9 6 6 6-6"/>',
 'box':'<path d="M3 7.5 12 3l9 4.5v9L12 21l-9-4.5z"/><path d="M3 7.5 12 12l9-4.5M12 12v9"/>',
 'chart':'<path d="M4 20V10M10 20V4M16 20v-7M22 20H2"/>',
 'users':'<circle cx="9" cy="8" r="3.5"/><path d="M2.5 20a6.5 6.5 0 0 1 13 0"/><path d="M16 4.5a3.5 3.5 0 0 1 0 7M18 14a6.5 6.5 0 0 1 3.5 6"/>',
 'msg':'<path d="M4 5h16v11H9l-5 4z"/>',
 'gift':'<rect x="3" y="8" width="18" height="4" rx="1"/><path d="M5 12v9h14v-9M12 8v13"/><path d="M12 8c-2-4-6-4-6-1.5S12 8 12 8s6 0 6-1.5S14 4 12 8z"/>',
 'bell':'<path d="M6 16v-5a6 6 0 0 1 12 0v5l2 2H4z"/><path d="M10 21h4"/>',
 'x':'<path d="M6 6l12 12M18 6 6 18"/>',
 'tag':'<path d="M3 12V4h8l10 10-8 8z"/><circle cx="7.5" cy="8.5" r="1.5"/>',
 'cog':'<circle cx="12" cy="12" r="3"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3M4.9 4.9 7 7M17 17l2.1 2.1M4.9 19.1 7 17M17 7l2.1-2.1"/>',
 'shield':'<path d="M12 3 4 6v6c0 5 3.5 8 8 9 4.5-1 8-4 8-9V6z"/>',
 'scan':'<path d="M4 7V4h3M17 4h3v3M20 17v3h-3M7 20H4v-3"/><path d="M8 8v8M11 8v8M14 8v8M17 8v8"/>',
 'info':'<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/>',
 'warn':'<path d="M12 3 2 20h20z"/><path d="M12 10v4M12 17h.01"/>',
 'nav':'<path d="m3 11 18-8-8 18-2-8z"/>',
}
def I(n, cls=''): return f'<svg class="tz-ico {cls}" viewBox="0 0 24 24" aria-hidden="true">{P[n]}</svg>'

def doc(marker, title, body, lang='en'):
    return f'''{marker}
<!doctype html>
<html lang="{lang}">
<head><meta charset="utf-8"><title>{title}</title></head>
<body>
{body}
</body>
</html>
'''
C = {}  # name -> (marker, readme, body)

C['Button'] = ('<!-- @dsCard group="Actions" height=152 -->', '''# Button

The one control for every action; `tz-btn-primary` at most once per view, for the thing the screen is for.

- Variants: default (outlined, `line-control` border on `surface`), `tz-btn-primary` (`brand` fill, `on-brand` label), `tz-btn-accent` (`lime` fill, `on-lime` label, only for celebratory or offer actions), `tz-btn-ghost` (text in `brand`), `tz-btn-danger` (`danger` fill, destructive only, after confirmation).
- Sizes: `tz-btn-sm` (36px, admin tables), default (44px), `tz-btn-lg` (52px, checkout and onboarding). `tz-btn-block` fills its container; use it for the bottom action on phone screens.
- `tz-btn-icon` makes a round 44px icon-only button; give it an `aria-label`.
- The consumer provides a verb-first, sentence-case label and an optional leading Lucide icon. Disabled buttons use the `disabled` attribute; explain why nearby.
''', f'''<div class="tz-stack">
<div class="tz-row" style="padding:0">
<button class="tz-btn tz-btn-primary">Place order</button>
<button class="tz-btn">{I('pin')}Directions</button>
<button class="tz-btn tz-btn-ghost">View all</button>
<button class="tz-btn tz-btn-accent">{I('gift')}Invite a friend</button>
<button class="tz-btn tz-btn-danger">Reject order</button>
<button class="tz-btn" disabled>Out of stock</button>
<button class="tz-btn tz-btn-icon" aria-label="Voice update">{I('mic')}</button>
</div>
<div class="tz-row" style="padding:0">
<button class="tz-btn tz-btn-primary tz-btn-lg">Charge ₹1,246</button>
<button class="tz-btn tz-btn-sm">Export CSV</button>
</div>
</div>''')

C['SearchField'] = ('<!-- @dsCard group="Inputs" height=88 -->', '''# SearchField

The pill-shaped search that leads the customer home screen and the POS product grid.

- Structure: `tz-search` wrapping a search icon, an `<input type="search">` and an optional trailing icon button (scan barcode in the POS, voice in the customer app).
- Placeholder names real things: "Search atta, milk, medicines…" for customers, "Search by name or scan barcode" in the POS.
- The consumer provides the input's `id`, the label (visually hidden is fine) and the handler. Results update as the person types after 2 characters, with a 200ms debounce.
''', f'''<div class="tz-stack">
<label class="tz-search" for="q">{I('search')}<input id="q" type="search" placeholder="Search atta, milk, medicines…"><span style="color:var(--brand)">{I('mic')}</span></label>
</div>''')

C['TextField'] = ('<!-- @dsCard group="Inputs" height=196 -->', '''# TextField

Label above, input, then help or error text below; used for every form in onboarding, checkout and settings.

- Structure: `tz-field` > `tz-field-label`, `tz-input`, then `tz-field-help` or `tz-field-error`. Add `tz-input-invalid` with the error.
- Phone numbers show a fixed "+91" prefix and a numeric keypad (`inputmode="numeric"`).
- Errors say what to fix: "Enter a 6-digit PIN code", never "Invalid input".
- The consumer provides `id`, `for`, the label and the validation message.
''', '''<div class="tz-stack" style="max-width:420px">
<div class="tz-field"><label class="tz-field-label" for="shop">Shop name</label><input class="tz-input" id="shop" value="Sri Lakshmi Provision Stores"><span class="tz-field-help">Shown to customers on search results and your shop page.</span></div>
<div class="tz-field"><label class="tz-field-label" for="pin">PIN code</label><input class="tz-input tz-input-invalid" id="pin" inputmode="numeric" value="5600"><span class="tz-field-error">Enter a 6-digit PIN code.</span></div>
</div>''')

C['Chip'] = ('<!-- @dsCard group="Inputs" height=72 -->', '''# Chip

Filter and radius chips in a horizontally scrolling row; `aria-pressed="true"` marks the selected ones.

- Use for search radius (1, 2, 5, 10 km), filters (Open now, Pickup, Delivery, Offers) and POS categories.
- Selected chips use `brand-soft` with `brand` text and border. Single-choice rows behave like radio buttons.
- The consumer provides the label, pressed state and handler; keep labels to two words.
''', '''<div class="tz-row"><div class="tz-chips">
<button class="tz-chip" aria-pressed="false">1 km</button>
<button class="tz-chip" aria-pressed="true">2 km</button>
<button class="tz-chip" aria-pressed="false">5 km</button>
<button class="tz-chip" aria-pressed="false">10 km</button>
<button class="tz-chip" aria-pressed="true">Open now</button>
<button class="tz-chip" aria-pressed="false">Delivery</button>
<button class="tz-chip" aria-pressed="false">Offers</button>
</div></div>''')

C['StatusPill'] = ('<!-- @dsCard group="Status" height=120 -->', '''# StatusPill

The fixed vocabulary of order and payment states, shown identically to customer, shop and admin.

| State | Class | Colour |
| --- | --- | --- |
| Pending | `tz-pill-pending` | `warning` on `warning-soft` |
| Accepted | `tz-pill-accepted` | `info` on `info-soft` |
| Preparing | `tz-pill-preparing` | `teal` on `teal-soft` |
| Ready for pickup | `tz-pill-ready` | `on-lime` on `lime` |
| Out for delivery | `tz-pill-delivery` | `info` on `info-soft` |
| Completed | `tz-pill-completed` | `brand` on `brand-soft` |
| Rejected, Cancelled | `tz-pill-rejected`, `tz-pill-cancelled` | `danger` on `danger-soft` |
| Unpaid | `tz-pill` | `ink-2` on `surface-sunken` |
| Paid | `tz-pill-paid` | `brand` on `brand-soft` |

- Order status and payment status are separate pills; show both on order cards.
- Always the dot and the word. Never shorten "Ready for pickup" to "Ready" on the customer side.
''', '''<div class="tz-stack">
<div class="tz-row" style="padding:0">
<span class="tz-pill tz-pill-pending">Pending</span>
<span class="tz-pill tz-pill-accepted">Accepted</span>
<span class="tz-pill tz-pill-preparing">Preparing</span>
<span class="tz-pill tz-pill-ready">Ready for pickup</span>
<span class="tz-pill tz-pill-delivery">Out for delivery</span>
<span class="tz-pill tz-pill-completed">Completed</span>
<span class="tz-pill tz-pill-rejected">Rejected</span>
<span class="tz-pill tz-pill-cancelled">Cancelled</span>
</div>
<div class="tz-row" style="padding:0">
<span class="tz-pill">Unpaid · pay at pickup</span>
<span class="tz-pill tz-pill-paid">Paid · UPI</span>
</div>
</div>''')

C['StockTag'] = ('<!-- @dsCard group="Status" height=120 -->', '''# StockTag

Availability plus freshness: the promise TodayZ makes, shown on every product everywhere.

- `tz-stock` (in stock, `brand`), `tz-stock-low` (`warning`, include the count: "Only 3 left"), `tz-stock-out` (`ink-2`, hollow dot).
- Always follow with `tz-fresh`: "updated 12 min ago". Over 24 hours old, replace the stock tag with "Stock may have changed" in `warning`.
- The consumer provides the stock count, the low-stock threshold and the last-updated time.
''', '''<div class="tz-stack">
<div><span class="tz-stock">In stock</span> <span class="tz-fresh">· updated 12 min ago</span></div>
<div><span class="tz-stock tz-stock-low">Only 3 left</span> <span class="tz-fresh">· updated 1 hr ago</span></div>
<div><span class="tz-stock tz-stock-out">Out of stock</span> <span class="tz-fresh">· updated today, 9:15 am</span></div>
</div>''')

C['QuantityStepper'] = ('<!-- @dsCard group="Commerce" height=72 -->', '''# QuantityStepper

The Add button that becomes a − 1 + stepper in place once the item is in the cart.

- Start as `tz-add` ("Add"). After the first tap, swap to `tz-stepper` in the same footprint so the layout does not jump.
- At zero, return to "Add". At the available stock, disable + and show "Only 3 left" below.
- Buttons need `aria-label`s ("Remove one", "Add one"); the count is announced politely.
''', f'''<div class="tz-row">
<button class="tz-add">Add</button>
<div class="tz-stepper"><button aria-label="Remove one">{I('minus','tz-ico-sm')}</button><span>2</span><button aria-label="Add one">{I('plus','tz-ico-sm')}</button></div>
</div>''')

PC = lambda ini,name,unit,price,mrp,off,stock,fresh,btn: f'''<article class="tz-pcard">
<div class="tz-thumb">{ini}</div>
<div class="tz-pcard-name">{name}</div>
<div class="tz-pcard-unit">{unit}</div>
<div>{stock}<div class="tz-fresh">{fresh}</div></div>
<div class="tz-pcard-foot"><div><div class="tz-price">{price}</div>{f'<span class="tz-mrp">{mrp}</span> <span class="tz-off">{off}</span>' if mrp else '<span class="tz-mrp" style="text-decoration:none">&nbsp;</span>'}</div>{btn}</div>
</article>'''
STEP = f'<div class="tz-stepper"><button aria-label="Remove one">{I("minus","tz-ico-sm")}</button><span>1</span><button aria-label="Add one">{I("plus","tz-ico-sm")}</button></div>'
ADD = '<button class="tz-add">Add</button>'
C['ProductCard'] = ('<!-- @dsCard group="Commerce" height=360 -->', '''# ProductCard

A product as one shop sells it: image, name, unit, stock with freshness, price with MRP, and Add.

- Two per row on phone (`tz-grid2`), 4–6 per row on desktop storefronts. Names clamp to two lines.
- Show MRP and the discount only when the shop's price is lower.
- In cross-shop search results, add the shop name and distance as a `tz-meta` line under the unit.
- Out of stock cards keep their place, dim the image and replace Add with "Notify me".
- The consumer provides image URL or initials, name, unit, price, MRP, stock state and timestamp, and cart quantity.
''', f'''<div class="tz-grid2" style="padding:var(--space-4);max-width:400px">
{PC('AT','Aashirvaad Shudh Chakki Atta','5 kg','₹289','₹320','10% off','<span class="tz-stock">In stock</span>','updated 12 min ago',STEP)}
{PC('AM','Amul Taaza Toned Milk','500 ml','₹28','','','<span class="tz-stock tz-stock-low">Only 3 left</span>','updated 1 hr ago',ADD)}
</div>''')

C['StoreCard'] = ('<!-- @dsCard group="Commerce" height=196 -->', '''# StoreCard

A nearby shop in lists and on the map sheet: photo or initials, name, distance, open state, fulfilment and how many results match.

- Tags: `tz-tag-open` for "Open till 10 pm" (or a plain `tz-tag` "Closed · opens 7 am"), `tz-tag` for Pickup and Delivery, `tz-tag-offer` for a live offer.
- The trailing chevron opens the shop page. A Directions action lives on the shop page, not the card.
- The consumer provides shop photo or initials, name, area, distance, hours, fulfilment options and match count.
''', f'''<div class="tz-stack" style="max-width:420px">
<a class="tz-scard" href="#" style="text-decoration:none;color:inherit"><div class="tz-avatar">SL</div><div style="min-width:0"><div class="tz-scard-name">Sri Lakshmi Provision Stores</div><div class="tz-meta"><span>0.6 km</span><span>Jayanagar 4th Block</span><span>· 42 items match</span></div><div class="tz-meta" style="margin-top:6px"><span class="tz-tag tz-tag-open">Open till 10 pm</span><span class="tz-tag">Pickup</span><span class="tz-tag">Delivery</span></div></div><span style="color:var(--ink-2)">{I('chev')}</span></a>
<a class="tz-scard" href="#" style="text-decoration:none;color:inherit"><div class="tz-avatar">SG</div><div style="min-width:0"><div class="tz-scard-name">Sharma General Store</div><div class="tz-meta"><span>1.2 km</span><span>JP Nagar 2nd Phase</span></div><div class="tz-meta" style="margin-top:6px"><span class="tz-tag">Closed · opens 7 am</span><span class="tz-tag tz-tag-offer">10% off on ₹500+</span></div></div><span style="color:var(--ink-2)">{I('chev')}</span></a>
</div>''')

C['CartBar'] = ('<!-- @dsCard group="Commerce" height=96 -->', '''# CartBar

The floating summary above the tab bar whenever the cart has items: count, total, shop, View cart.

- Sits `space-3` above the bottom nav with `shadow-pop`, full width minus the gutter, `radius-xl`.
- Shows the shop name because a cart belongs to one shop.
- Hidden on the cart and checkout screens themselves.
- The consumer provides item count, total and shop name.
''', f'''<div class="tz-stack" style="max-width:400px">
<div class="tz-cartbar"><span class="tz-cartbar-count">3</span><span class="tz-cartbar-text">₹345<small>from Sri Lakshmi Provision Stores</small></span><button class="tz-cartbar-cta">View cart{I('chev','tz-ico-sm')}</button></div>
</div>''')

C['BottomNav'] = ('<!-- @dsCard group="Navigation" height=168 -->', '''# BottomNav

The phone tab bar for the customer app and the business app on phone.

- Customer: Home, Search, Orders, Wallet, Account. Business: Overview, Orders, Sell, Stock, More.
- Five items at most, icon over a one-word label, active item in `brand` with a heavier stroke.
- Badge counts (new orders) use a small `danger` dot with the number.
- Height `bottom-nav` plus the safe-area inset.
''', f'''<div class="tz-stack" style="max-width:400px;gap:var(--space-4)">
<nav class="tz-bnav" aria-label="Customer"><a href="#" aria-current="page">{I('home')}Home</a><a href="#">{I('search')}Search</a><a href="#">{I('receipt')}Orders</a><a href="#">{I('wallet')}Wallet</a><a href="#">{I('user')}Account</a></nav>
<nav class="tz-bnav" aria-label="Business"><a href="#">{I('chart')}Overview</a><a href="#" aria-current="page">{I('receipt')}Orders</a><a href="#">{I('scan')}Sell</a><a href="#">{I('box')}Stock</a><a href="#">{I('cog')}More</a></nav>
</div>''')

C['Sidebar'] = ('<!-- @dsCard group="Navigation" height=520 -->', '''# Sidebar

The desktop navigation for the business app and the admin console, on a `forest` band.

- Brand at top, then grouped links with `overline`-styled group labels. Active link is a `lime` fill with `on-lime` text.
- Counts (new orders, open tickets) sit right-aligned in the link.
- Collapses to the BottomNav below 1024px for the business app; the admin console collapses to icons.
''', f'''<div style="display:flex;height:520px">
<nav class="tz-side" aria-label="Business">
<div class="tz-side-brand"><span>Today<b>Z</b></span></div>
<a href="#">{I('chart')}Overview</a>
<a href="#" aria-current="page">{I('receipt')}Orders<span class="tz-count">3</span></a>
<a href="#">{I('scan')}Sell</a>
<a href="#">{I('box')}Stock</a>
<a href="#">{I('users')}Customers</a>
<div class="tz-side-group">Grow</div>
<a href="#">{I('tag')}Offers</a>
<a href="#">{I('gift')}Referrals and wallet</a>
<a href="#">{I('chart')}Analytics</a>
<div class="tz-side-group">Shop</div>
<a href="#">{I('store')}Stores and staff</a>
<a href="#">{I('msg')}Help</a>
<a href="#">{I('cog')}Settings</a>
</nav></div>''')

C['KpiTile'] = ('<!-- @dsCard group="Dashboard" height=150 -->', '''# KpiTile

One number with its label and its change, for business and admin dashboards.

- Label in `overline` style, value in `kpi`, delta in `caption` weight: up in `brand` with "▲", down in `danger` with "▼", always with the comparison ("vs yesterday").
- Four across on desktop, two across on phone. Do not open a page with more than four.
- The consumer provides label, formatted value, delta and comparison period.
''', '''<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:var(--space-3);padding:var(--space-4)">
<div class="tz-kpi"><span class="tz-kpi-label">Sales today</span><span class="tz-kpi-value">₹48,260</span><span class="tz-kpi-delta">▲ 12% vs yesterday</span></div>
<div class="tz-kpi"><span class="tz-kpi-label">Orders</span><span class="tz-kpi-value">37</span><span class="tz-kpi-delta">▲ 5 vs yesterday</span></div>
<div class="tz-kpi"><span class="tz-kpi-label">Items sold</span><span class="tz-kpi-value">214</span><span class="tz-kpi-delta tz-kpi-delta-down">▼ 3% vs yesterday</span></div>
<div class="tz-kpi"><span class="tz-kpi-label">Low stock</span><span class="tz-kpi-value">9</span><span class="tz-kpi-delta" style="color:var(--warning)">5 need reorder</span></div>
</div>''')

C['PickupCode'] = ('<!-- @dsCard group="Commerce" height=300 -->', '''# PickupCode

The 4-digit code a customer shows at the counter, and the four-box input the shop types it into.

- Customer side: `tz-code` on `lime`, digits in `otp`, the shop name and "Show this code at the counter". Brightness hint: the screen should stay awake while it is shown.
- Shop side: `tz-otp`, four numeric inputs that auto-advance and accept paste. Wrong code shakes once and says "That code doesn't match order TZ-24817".
- Codes are single-use and expire when the order closes.
''', '''<div class="tz-row" style="align-items:flex-start;gap:var(--space-6)">
<div class="tz-code" style="width:260px"><span style="font:600 14px/20px var(--font-sans)">Pickup code</span><span class="tz-code-digits">4827</span><span style="font:500 13px/18px var(--font-sans)">Show this at Sri Lakshmi Provision Stores</span></div>
<div class="tz-field"><label class="tz-field-label" for="o1">Enter customer's code</label><div class="tz-otp"><input id="o1" inputmode="numeric" maxlength="1" value="4"><input inputmode="numeric" maxlength="1" value="8" aria-label="Digit 2"><input inputmode="numeric" maxlength="1" value="2" aria-label="Digit 3"><input inputmode="numeric" maxlength="1" aria-label="Digit 4"></div><span class="tz-field-help">Order TZ-24817 · Ananya R.</span></div>
</div>''')

C['OrderTimeline'] = ('<!-- @dsCard group="Status" height=300 -->', '''# OrderTimeline

Every status an order passes through, time-stamped, identical on the customer and shop sides.

- Done steps get a filled `brand` dot with a check; the current step a `lime` dot; upcoming steps are hollow with `ink-2` text.
- Pickup orders: Placed, Accepted, Preparing, Ready for pickup, Collected. Delivery orders swap the last two for Out for delivery, Delivered.
- Rejected or cancelled orders end the list with a `danger` step and the reason.
''', f'''<div class="tz-stack" style="max-width:360px">
<ol class="tz-steps">
<li data-done><span class="tz-dot">{I('check','tz-ico-sm')}</span><div><b>Order placed</b><small>6:02 pm</small></div></li>
<li data-done><span class="tz-dot">{I('check','tz-ico-sm')}</span><div><b>Accepted by the shop</b><small>6:04 pm</small></div></li>
<li data-done><span class="tz-dot">{I('check','tz-ico-sm')}</span><div><b>Preparing</b><small>6:05 pm</small></div></li>
<li data-now><span class="tz-dot">{I('bag','tz-ico-sm')}</span><div><b>Ready for pickup</b><small>6:18 pm · show your code at the counter</small></div></li>
<li><span class="tz-dot"></span><div><b>Collected</b></div></li>
</ol>
</div>''')

C['Banner'] = ('<!-- @dsCard group="Status" height=210 -->', '''# Banner

Inline notices for the page (Banner) and short confirmations that disappear (Toast).

- `tz-banner` (info) and `tz-banner-warning`: one icon, a bold first phrase, one sentence, an optional link. Use for integration-off notices, stale stock and offline mode.
- `tz-toast` on `forest`: one line plus an optional Undo or View in `lime`. 4 seconds, bottom of the screen above the nav.
- Never use either for errors a person must fix in a form; put those next to the field.
''', f'''<div class="tz-stack" style="max-width:560px">
<div class="tz-banner tz-banner-warning">{I('warn')}<span><b>WhatsApp is off.</b> Order updates go out as in-app and SMS only. Add a WhatsApp API key in Settings to turn it on.</span></div>
<div class="tz-banner">{I('info')}<span><b>You're offline.</b> Sales you record now will sync when you're back online.</span></div>
<div><span class="tz-toast">{I('check')}Sale recorded · ₹1,246 <a href="#">View</a></span></div>
</div>''')

C['DataTable'] = ('<!-- @dsCard group="Dashboard" height=330 -->', '''# DataTable

Dense, sortable lists for stock, orders, customers and admin records.

- Header row in `overline` style on `surface-sunken`; rows 52px with `line` dividers; numbers right-aligned with tabular figures.
- First column carries the name with a `tz-sub` second line (SKU, phone, area).
- Wrap in `tz-table-wrap` so wide tables scroll inside their card on phone.
- Row click opens a detail drawer; bulk actions appear in a bar when rows are selected.
''', '''<div style="padding:var(--space-4)"><div class="tz-table-wrap"><table class="tz-table">
<thead><tr><th>Product</th><th class="num">Price</th><th class="num">Stock</th><th>Last change</th><th>Status</th></tr></thead>
<tbody>
<tr><td>Aashirvaad Shudh Chakki Atta<span class="tz-sub">5 kg · Staples</span></td><td class="num">₹289</td><td class="num">24</td><td>POS sale<span class="tz-sub">12 min ago</span></td><td><span class="tz-stock">In stock</span></td></tr>
<tr><td>Fortune Sunlite Sunflower Oil<span class="tz-sub">1 L · Oils</span></td><td class="num">₹165</td><td class="num">3</td><td>Online order<span class="tz-sub">1 hr ago</span></td><td><span class="tz-stock tz-stock-low">Low</span></td></tr>
<tr><td>Tata Salt<span class="tz-sub">1 kg · Staples</span></td><td class="num">₹28</td><td class="num">0</td><td>Voice update<span class="tz-sub">Today, 9:15 am</span></td><td><span class="tz-stock tz-stock-out">Out</span></td></tr>
<tr><td>Maggi 2-Minute Noodles<span class="tz-sub">4 × 70 g · Snacks</span></td><td class="num">₹56</td><td class="num">40</td><td>Delivery received<span class="tz-sub">Yesterday</span></td><td><span class="tz-stock">In stock</span></td></tr>
</tbody></table></div></div>''')

C['PosCart'] = ('<!-- @dsCard group="Commerce" height=470 -->', '''# PosCart

The right-hand cart of the counter POS: customer, lines, discount, payment method and the Charge bar.

- Customer lookup by phone at the top; walk-in is the default.
- Each `tz-pos-line`: name with unit, a stepper, line total. Tap a line to change price or remove.
- Payment method chips: Cash, UPI, Card, Credit (khata). The `tz-pos-total` bar on `forest` holds the total in `display` and the Charge button in `lime`... use `tz-btn-accent` there because `brand` would vanish against forest.
- Recording the sale reduces stock immediately and shows a Toast.
''', f'''<div style="padding:var(--space-4);max-width:400px"><div style="background:var(--surface);border-radius:var(--radius-lg);box-shadow:var(--shadow-card);padding:var(--space-4);display:flex;flex-direction:column;gap:var(--space-3)">
<div class="tz-search" style="min-height:44px">{I('user')}<input aria-label="Customer phone" placeholder="Customer phone (optional)" value="98450 14521"></div>
<div style="font:500 13px/18px var(--font-sans);color:var(--ink-2)">Ananya R. · 6 visits · 420 points</div>
<div>
<div class="tz-pos-line"><div>Aashirvaad Atta<span class="tz-sub">5 kg · ₹289</span></div><div class="tz-stepper" style="height:32px">{'<button aria-label="Remove one" style="width:32px;height:32px">'+I('minus','tz-ico-sm')+'</button>'}<span>2</span>{'<button aria-label="Add one" style="width:32px;height:32px">'+I('plus','tz-ico-sm')+'</button>'}</div><b class="tz-num">₹578</b></div>
<div class="tz-pos-line"><div>Fortune Sunflower Oil<span class="tz-sub">1 L · ₹165</span></div><div class="tz-stepper" style="height:32px">{'<button aria-label="Remove one" style="width:32px;height:32px">'+I('minus','tz-ico-sm')+'</button>'}<span>3</span>{'<button aria-label="Add one" style="width:32px;height:32px">'+I('plus','tz-ico-sm')+'</button>'}</div><b class="tz-num">₹495</b></div>
<div class="tz-pos-line"><div>Amul Taaza Milk<span class="tz-sub">500 ml · ₹28</span></div><div class="tz-stepper" style="height:32px">{'<button aria-label="Remove one" style="width:32px;height:32px">'+I('minus','tz-ico-sm')+'</button>'}<span>6</span>{'<button aria-label="Add one" style="width:32px;height:32px">'+I('plus','tz-ico-sm')+'</button>'}</div><b class="tz-num">₹168</b></div>
</div>
<div class="tz-chips"><button class="tz-chip" aria-pressed="false">Cash</button><button class="tz-chip" aria-pressed="true">UPI</button><button class="tz-chip" aria-pressed="false">Card</button><button class="tz-chip" aria-pressed="false">Credit</button></div>
<div class="tz-pos-total"><div><div style="font:500 12px/16px var(--font-sans);color:var(--on-forest-2)">11 items · ₹5 discount</div><div class="tz-amt">₹1,236</div></div><button class="tz-btn tz-btn-accent tz-btn-lg">Charge</button></div>
</div></div>''')

C['WalletCard'] = ('<!-- @dsCard group="Commerce" height=230 -->', '''# WalletCard

The points balance on a `forest` card, with what it is worth and how to earn more.

- Points in `lime` at `otp` size; the rupee value under it ("Worth ₹42 off your next order").
- Points are credits, not cash: never show a withdraw action.
- Below the card, list recent earnings and redemptions; referral invite sits as a secondary action.
''', f'''<div style="padding:var(--space-4);max-width:400px"><div class="tz-wallet"><small>TodayZ points</small><span class="tz-wallet-pts">420</span><small>Worth ₹42 off your next order at any TodayZ shop</small><div style="display:flex;gap:var(--space-2);margin-top:var(--space-2)"><button class="tz-btn tz-btn-accent tz-btn-sm">{I('gift','tz-ico-sm')}Invite and earn 50</button></div></div></div>''')

C['LanguagePicker'] = ('<!-- @dsCard group="Inputs" height=260 -->', '''# LanguagePicker

The first onboarding step for customers and shops: each language written in its own script, with its English name below.

- Options: English, हिन्दी (Hindi), ಕನ್ನಡ (Kannada), తెలుగు (Telugu), தமிழ் (Tamil). Pre-select from the phone's language when it matches.
- The selected option uses `brand-soft` with a `brand` border. Changing it re-renders the screen immediately.
- The same control appears in Account and Settings.
''', '''<div style="padding:var(--space-4);max-width:520px"><div class="tz-lang">
<button aria-pressed="true"><span class="native" lang="en">English</span><small>English</small></button>
<button aria-pressed="false"><span class="native" lang="hi">हिन्दी</span><small>Hindi</small></button>
<button aria-pressed="false"><span class="native" lang="kn">ಕನ್ನಡ</span><small>Kannada</small></button>
<button aria-pressed="false"><span class="native" lang="te">తెలుగు</span><small>Telugu</small></button>
<button aria-pressed="false"><span class="native" lang="ta">தமிழ்</span><small>Tamil</small></button>
</div></div>''')

for name,(marker,readme,body) in C.items():
    d=os.path.join(ROOT,'components',name); os.makedirs(d,exist_ok=True)
    open(os.path.join(d,'README.md'),'w').write(readme)
    open(os.path.join(d,'preview.html'),'w').write(doc(marker,f'{name} — preview',body))
print(len(C),'components')
import json; json.dump(P, open(os.path.join(os.path.dirname(__file__),'icons.json'),'w'))
