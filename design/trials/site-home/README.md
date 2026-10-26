# Site home trial: cream and navy (D-246)

A standalone, local-only try-out of the founder's landing page (`TodayZ — The Physical World, Searchable`) in our own design system. Nothing here is built, deployed or imported: tokens, `shared/ui`, the apps and the live `site/` are unchanged.

## Open it

The page reads our generated fonts and tokens, so generate them once:

```
pnpm --filter @todayz/tokens gen
```

Then open `design/trials/site-home/index.html` in a browser (double-click is fine; `make gen` also works).

## What it keeps from the founder's page

Every section, in the same order, with the same copy:

- top bar, header (search, area, wallet, CTA)
- hero with search and the three stats
- the two moving rows of searches
- the problem cards and "The TodayZ way"
- the journey map with its counters and four steps
- "Errands, reimagined" with activity rings and weekly bars
- the 1.5 km radar with the store card
- the shelf inspector with category tabs and products
- the savings calculator, the store owners block and the footer
- the four pop-ups: search, list your store, choose area, early access

## What changed

- **Look.** Cream background with the dot grid, and navy for the top bar, radar, calculator, store owners block and footer. Our bag logo in a white tile with the `TodayZ` wordmark (the PNG has a white background, so it always sits on white).
- **Type and icons.** Anek for headings, Figtree for text. No emoji: Lucide icons on our pastel tints.
- **Colour.** Text and buttons on cream use our brand green (`#067a3c`, 5.4:1). The source emerald `#059669` fails contrast on cream (3.6:1), so it is not used for text. Mint and the category colours appear only on navy.
- **Fixes.**
  - The "Errands" section had no styles in the source; it is styled here.
  - The header no longer overflows at 1280 px.
  - The hero stats and top bar fit at 360 px.
  - Every control is a real button or link, at least 44 px where it is a main control.
  - Pop-ups are native dialogs (focus stays inside, Escape closes).
  - Form fields have labels.
  - The moving rows have a pause button.
  - Radar stores can be reached by keyboard.
  - Loops run a few times and stop; the walk replays on a button.
  - Reduced motion shows the finished state.
  - No page text is smaller than 11 px (labels inside the map, radar and rings scale with the drawing).
- **Dropped.** Dark mode and the download button (we are light only).

Checked at 1280 px and 360 px: no sideways scroll, axe (WCAG 2.2 AA and best practice) finds nothing.

## Trial-only values

Set as CSS variables at the top of `index.html`, not as tokens yet:

| Name | Value | Use |
|---|---|---|
| `--cream` | `#faf9f5` | page background |
| `--cream-2` | `#f3f0e9` | soft fills, bands |
| `--navy` | `#0b101d` | dark blocks |
| `--navy-accent` | `#6ee7b7` | text accents on navy (12.5:1) |
| `--navy-green` / `--navy-pink` / `--navy-blue` | `#34d399` / `#f472b6` / `#60a5fa` | radar dots, sliders |
| `--amber` | `#d97706` | decoration only (rings, map pin, low-stock bar) |

## Open copy and product questions

The page keeps the founder's copy word for word. These lines don't match `docs/plan/01-product-rules.md` and need a decision before the page goes live:

1. **"Up to 2% cashback" and "2%" in the stats and calculator.** Cashback is set per store, from 0 to 20%, default 0. A fixed platform figure isn't a product rule.
2. **"₹0 delivery and handling fees" and "No delivery or handling fees".** Stores can deliver and set their own delivery fee and free-above amount. Pickup has no fee; delivery may.
3. **"Earn ₹30 for every customer you bring".** Referral rewards are credits, set in `ref_<kind>_*` (30 to the referrer, 100 to the new person by default), not rupees.
4. **"Verified stock promise", "98% available", "Only 3 left".** The stock tag reads "Confirmed {time} ago" (D-101). We don't promise availability percentages.
5. **"Explore the 1.5 km around you".** Radius chips are 1, 2, 5, 10 and 20 km, default 2.
6. **"Physical search engine", step counter, calories.** No step or calorie tracking exists in the product.
7. **Marquee searches.** "HVAC repair quotes" and "Wholesale coffee beans 5 kg" are services or wholesale, outside what shops list.
8. **Sample cities.** Mumbai, Delhi and Hyderabad appear in the area picker.

## If the founder keeps it

1. Add `cream`, `cream-2`, `navy`, the on-navy accents, a larger radius and a lift shadow to `design/source/tokens.json`.
2. Port the sections into `site/` as Astro components, with every string as a message key and no client JS beyond what the site allows.
3. Settle the questions above, then refresh the site's visual baselines.
