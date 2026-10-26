TodayZ makes the shops around you searchable. A customer checks what a nearby kirana, pharmacy or supermarket actually has on the shelf right now, reserves it, and picks it up or gets it delivered. The shop runs its counter, stock and customers from the same app. One design system serves four surfaces: the **customer app** (phone only), the **business app** (phone and desktop, including the POS), the **admin console** (desktop first) and, later, **storefronts** for each business.

The brand idea is *today, nearby, for real*: live stock from real shops within a short walk or ride. Everything in the system leans toward freshness (when was this stock last updated?), distance (how far is the shop?) and trust (will it be there when I arrive?).

## Voice and copy

- Write like a helpful shopkeeper: short, plain, warm. "Ready for pickup", not "Your order has been successfully processed".
- Sentence case everywhere, including buttons and tabs. Verbs first on buttons: "Add to cart", "Accept order", "Record sale".
- Address the reader as "you". The shop is named by its name ("Sri Lakshmi Provision Stores"), never "the merchant".
- Money is always rupees with the ₹ sign, Indian digit grouping and no decimals unless paise matter: ₹1,24,500 in dashboards, ₹289 on products, ₹1,246.00 only on POS receipts.
- Distances in km with one decimal under 10 km: "0.6 km", "2.4 km". Times relative when fresh ("12 min ago"), absolute when old ("Yesterday, 6:40 pm").
- Every stock figure carries its freshness: "In stock · updated 12 min ago". Never show availability without a timestamp.
- No emoji in UI copy. No exclamation marks except the first-order celebration.
- Product names stay as the shop or master list writes them (usually English); all interface text is translated. See the Localization section.

## Colour

White first, black and grey for structure, a pale mint and soft pastels for warmth, and brand green only in a few places. The page stays white like Marcel Kargul's dashboards, but TodayZ adds light colour on top so screens never feel plain.

- **The page is white.** `bg` is #FFFFFF, and cards, tables, panels and the sidebar are white `surface` too. What separates them is a 1px `line` border, never a grey ground or a card shadow. `surface-subtle` (#FAFAFA) is the faintest grey, for table header rows, the KPI footer band and row hover. `surface-sunken` (#F4F4F4) is for the segmented track, the active sidebar item, soft buttons and image wells. Control borders are `line-control`.
- **Light colour comes in two families, placed on the white page:**
  - **Mint `wash`** (`wash`, `wash-strong`, `wash-line`): hero bands that fade to white (`tz-hero-wash` at the top of the customer home, `tz-greet` around the business greeting, marketing sections), the active sidebar item, the hover row in the sidebar, the KPI lead cell and the setup card.
  - **Pastel tints** (`tint-green`, `tint-amber`, `tint-rose`, `tint-sky`, `tint-lilac`, `tint-peach`, each with its own `-ink` at 6:1 or better): category tiles, shop letter-avatars by category and Demand insights rows. Set them with `data-tint`.
  - Never tint a whole page or a whole content card, and never use a tint to mean a status: status keeps its dot pills. One washed band per screen.
- **Text** is near-black `ink`, with `ink-2` for secondary text and `ink-3` for placeholders and group labels.
- **The solid fill is `inverse`:** near-black. It covers primary buttons, selected chips, the quantity stepper, toasts, tooltips, the POS total bar, the pickup code and the wallet card. Text on it is `on-inverse`.
- **`brand` green is the accent.** Spend it only on:
  - Success and freshness signals: the dot on "In stock", "Stock updated 4 min ago" and "Open now"; the Ready pill; completed ticks on the timeline; "Synced"; a rise in a KPI.
  - The chart line and the highlighted bar.
  - Progress fills.
  - The icon of the active sidebar item.
  - The Z in the logo.
  - The customer's cart bar and checkout button (`tz-btn-brand`), plus the Charge button on the POS. These are the moments when money changes hands.
- **Never use brand green as** a page, card, sidebar or header fill (the pale mint `wash` is not brand green). If a screen shows more than about three green things at once, remove one.
- **Product photos** and initials sit on neutral `media`.
- **Status pills are neutral:** white, a hairline border, `ink` text and a coloured dot. Pending `warning`, accepted and out for delivery `info`, preparing `teal`, completed and paid `brand`, rejected and cancelled `danger`. Only "Ready for pickup" gets a green tint (`brand-soft` with a faint green border), because it is the moment the customer acts on. Every pill has a dot and a word.
- **Stock states:** in stock is a `brand` dot, low a `warning` dot, out of stock a hollow grey dot. Text stays `ink`.
- **Light only.** TodayZ ships one theme: white. There is no dark mode at launch; every app stays light whatever the phone or computer is set to.
- **Focus** is a 2px solid `focus` ring offset 2px, on every interactive element.

### Bright skin (customer app D-249, business app D-261)

The customer and business apps set `data-skin="bright"` on their root, and the skin in `components/bundle.css` changes these rules for those apps only. Admin and the design cards keep the rules above. In the business app, KPI tiles, tables, POS tiles and the greeting card also lift off the page with `shadow-card`, and the sidebar's active item is `brand-soft` with `brand-strong` text, like the active tab. Tabs and the segmented control keep their look in both apps.

- **The page is light grey `page` (#F4F5F7).** Cards stay white and lift off it with `shadow-card` instead of a `line` border. Only `ink` and `ink-2` text sits directly on the grey; `ink-3` would fall to 4.3:1.
- **Primary buttons are `brand` green, not `inverse`.** Brand stays the one loud colour; secondary buttons stay white and outlined.
- **Selected chips** are `brand-soft` with a `brand` border and `brand-strong` text.
- **Plain links** (an `a` with no class) are `brand` at weight 600.
- **The active tab** is `brand`, with a 3px brand bar on top.
- **The soft danger button** is white and outlined, with `danger` text.
- **Page titles** are Anek 800 at 26px.
- **Settings lists** (`tz-list`, `tz-list-row`, `tz-list-icon`, `tz-list-text`) are a white card of rows. Each row has a round tinted icon (`data-tint`), a label with its value, and a chevron when it opens something.
- **Section labels** (`tz-label-rule`) are letter-spaced capitals centred between hairlines that fade out.
- **Search** is 48px tall with a 12px corner, a soft shadow and a `brand` search icon.
- **Category tiles** are 64px circles with a white ring and a soft shadow.
- **Offer tags** (cashback) are `info` text on `info-soft`.
- **Home's shop cards** use the feature layout (`StoreCard layout="feature"`, `tz-scard-feature`): the shop's photo across the top, then the name in Anek 800. Without a photo, the initials sit on a shorter band in the category tint. Search and the map sheet keep the row layout.
- **Feature card overlays:** an open or closed tag on the photo (`photoTag`), the cashback in white over the `scrim-photo` gradient (`highlight`), a green rating badge (`rating`) and a dashed footer that says what the shop gives back (`footer`).
- **Home's header** shows the locality (from the reverse lookup) with the area line under it, a `brand-soft` credits pill and a round `tint-sky` avatar. The search bar sticks under it.
- **Home's offers row** is a swipe row of photo banners with dots. The "how it works" card is `brand`; shop offers and the credits and baskets cards cycle `promo-plum`, `promo-blue` and `promo-rose`, with white text and a white pill button in the banner's colour. The photo fades in from the left with a mask.
- **Home's categories** are a 3-column grid of round photos (`CategoryRow layout="grid"`, `imageUrl`).
- **Stock photos** for categories and the static banners are CC0, bundled as small WebP files in `customer/src/home/photos/` (sources in `SOURCES.md`). A shop's own photo or banner image always wins over a stock photo.

## Typography

- **Anek** (`display` family) for headlines and codes. It has Latin, Devanagari, Kannada, Telugu and Tamil cuts drawn as one family, so a Kannada heading feels as designed as an English one. Styles: `display-xl`, `display`, `headline`, `otp`.
- **Figtree** (`sans` family) for everything else, with Noto Sans Devanagari, Kannada, Telugu and Tamil behind it per script. Styles: `kpi`, `title`, `heading`, `body-lg`, `body`, `body-sm`, `label`, `caption`, `overline`, `price`, `price-lg`.
- Business and admin apps use `body` (14px), as Kargul Studio's dashboards do. The customer app sets `data-density="touch"`: 15px text, 16px inputs so iOS never zooms, and 44px controls.
- Weights stay light: 400 for text, 500 for labels, nav and numbers, 600 for titles. Bold (700) is for the pickup code only. Page titles are `title` (22px, 600) with a 14px `ink-2` subline; card titles are `heading` (15px, 600) with an optional 13px description.
- KPI values use Figtree, not Anek, so dashboard columns of numbers get true tabular figures. Prices, quantities, totals and KPIs use tabular numbers (`tz-num` or the `price`, `kpi` styles).
- `overline` (11px, uppercase, 0.06em tracking) is for sidebar group labels only. Table headers are sentence case, 13px, `ink-2`.

## Layout and spacing

- 4px grid: `space-1` to `space-12`. Phone gutter is `space-4`; desktop page padding `space-6` to `space-8`.
- Customer app: one column, max `phone-max` wide even on a laptop, bottom tab bar (`bottom-nav`), sticky cart bar above it. Thumb zone first: primary actions live in the bottom third.
- Business app: bottom tab bar on phone (Overview, Orders, Sell, Stock, More); a white `sidebar` with a hairline edge + content up to `content-max` on desktop, with 32px items, 16px icons and a mint `wash-strong` active item with a `brand` icon. The admin console uses the same sidebar with an "Admin" tag next to the logo. The same screens reflow; nothing is desktop-only except bulk catalogue editing.
- Control heights follow Kargul Studio: `control-md` (36px) is the default in the business and admin apps, `control-sm` (32px) for small buttons, icon buttons, filters and the segmented control, `control-lg` (44px) on touch screens and `control-xl` (48px) for checkout. Card padding is 20px, table rows 44px with a 36px header.
- Touch targets are at least `tap-min` (44px). POS keys and tiles use `pos-key` (64px).

## Shape, depth and motion

- Corners are tight and even: `radius-sm` (8px) for inputs, small buttons, icon buttons and sidebar items, `radius-md` (10px) for default buttons, search and toasts, `radius-lg` (12px) for cards and tables, `radius-xl` (20px) for sheet tops and the cart bar, `radius-pill` for category chips and the stepper. Pills, tags and count badges use `radius-xs` (6px).
- Nested corners are concentric: an outer radius equals the inner radius plus the padding between them (a 12px button in a card with 8px inset sits in a 20px corner). Past 24px of padding, treat the layers as separate and keep each token.
- Cards never have a shadow; a `line` border is enough on white. Controls get a whisper of depth: outlined buttons, inputs and the selected segment use `shadow-xs`; filled buttons (primary, brand, danger) use `shadow-btn`, a soft drop plus an inner top highlight, as on Kargul Studio's buttons. Menus and the cart bar use `shadow-pop`; bottom sheets `shadow-sheet`.
- Separators inside a card (above a card footer, between groups in a drawer) are dashed `line-control` (`tz-sep`).
- Motion is quick and functional, on one curve, `cubic-bezier(0.2, 0, 0, 1)`: 150ms for state changes, 220ms for sheets sliding up, buttons scale to 0.96 while pressed, a single 400ms check animation when an order is placed or a sale is recorded. Respect `prefers-reduced-motion` by cutting to fades.

## Iconography

- Use **Lucide** icons, outline style, 1.5px stroke so they read thin, as in Kargul Studio's work. 16px in all dense UI (sidebar, buttons, inputs, filters, tables, KPI names, pills); 20px on phone app bars and the bottom tab bar. Icons are `ink-2` or `ink-3` and inherit `currentColor`; only the active sidebar icon is `brand`.
- Icon-only buttons are outlined squares (`tz-btn-icon`, 32 or 36px, `radius-sm`), never circles. Empty states and invites put a 16px icon in a 40px outlined circle (`tz-iconring`). Activity rows use a 32px outlined square (`tz-kpi-icon`). Initials for shops and brands without a logo sit in small bordered grey squares (`tz-lettermark`).
- Icons in these previews are hand-drawn stand-ins in Lucide's style; the apps use the Lucide package itself.
- Product imagery comes from the master catalogue or the shop's own photos, shown on `surface-sunken` in a `radius-md` square. When there is no photo, show the product's initials in `display` on `surface-sunken`; never a stock illustration.

## Logo

- Use `todayz-logo.jpg` on light grounds. Keep clear space equal to the bag's handle height on every side.
- `todayz-bag-mark.png` is the app icon and favicon source; on small sizes use the bag alone, never the wordmark.
- Never recolour, outline or redraw the mark. In the sidebar the wordmark is set in `ink` with the Z in `brand-mark` (`tz-side-brand`) until an SVG logo exists.
