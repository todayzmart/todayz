# Design system (source copy)

Live artifact (source of truth for visuals): https://claude.ai/artifact/W91M8EtFX8zJc9SgMUygZF (v8, published version `1792652339-d9fd`).

- `source/` is a copy of the artifact's project files: `tokens.json`, `components/*/` (bundle.css, previews, READMEs), `patterns.md`, `localization.md`, logos.
- `generators/` holds the Python scripts that produced the previews. Order: `screens.py`, then `insights.py` (screens.py overwrites BusinessOverview), then `harness.py`.
- `shared/tokens` (Wave 0) generates CSS variables and TS constants from `source/tokens.json`. If the artifact changes, re-copy it here first.
- `artifact/` is a full snapshot of the rest of the artifact: page shell, the Design System type's viewer runtime, asset ids and sha256 manifests. See `artifact/README.md`.

## Where the plan differs from the design

`source/` stays a byte-identical copy of the artifact, so the differences are recorded here.

- **DemandInsights (D-99).** The "Wanted nearby" row drops the nearby-shop count: the product never shows how many shops stock an item and never names one. It shows the search count, plus "No shop nearby has it." when at least 10 other live verified shops of distinct businesses are in the radius and none lists the item. The product adds seven kinds to the design's four, and a "Not useful" action on each row (not on "Expired on shelf"). See `docs/plan/01-product-rules.md` §12.
- **Not in the design yet; each needs a design pass before it is built (D-101):**
  - **Ask sheet (C-25):** bottom sheet with the item, a quantity stepper, "Need by" chips (Now, Today, Tomorrow), the privacy line and Send.
  - **Ask progress and replies (C-26):** progress line "Asked 8 shops · 3 replied · best ₹38", time left, Yes cards with price, distance, "Ready by", "Replied 4 min ago", a "Ready soonest" badge and Book with a price-valid countdown.
  - **Asks inbox and reply sheet (B-20):** a business-app list of asks with the distance band and time left, and a one-tap Yes or No sheet with the price (locked for cashiers), "You have {n}", "Ready by" and Report.
  - **Result badges and freshness:** a "Lowest price" or "Closest" badge (at most one per card), the "Confirmed 12 min ago" stock tag and "+₹20 delivery" on the result card.
  - **Summary line, refine chips and mic:** "Checked 6 shops nearby · 3 have it · from ₹38" above results, removable filter chips, and a mic in the search field with its listening and transcript states.
  - **Share sheet and share card preview:** a small sheet (WhatsApp first, More, Copy link) and the link preview image with item, shop, price and area, never a distance.
  - **Demand (A-23):** an admin table of demand gaps per city, and the site strip approval list with Approve.
  - **Site sections (S-01, S-07):** the rotating "Just ask and find {item}" hero line, the "What people near you are searching for" strip with a city picker, and the comparison section; the dot-grid background and black pill CTA are optional accents inside the system.
