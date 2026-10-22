# DemandInsights

The business overview card the founders liked on the old site, rebuilt in the system. Four signals, each a tinted row with one action.

| Row | Tint | Icon | Signal | Action |
|---|---|---|---|---|
| Selling fast | `green` | trending-up | sales velocity vs stock: days until it runs out | Restock (opens reorder list) |
| Sitting on the shelf | `amber` | hourglass | in stock, no sale in N days (default 21) | Create offer |
| Going to waste | `rose` | alert-triangle | batches expiring within N days (default 5) | Flash offer (offer to nearby customers) |
| Wanted nearby | `sky` | search | customer searches within 2 km for items this shop does not stock, and how many nearby shops do | Add product |

- Each row: 32px tinted icon square, a 13px/600 heading, one sentence with the product in 500 `ink`, and a text action with →.
- Show at most one row per signal on the overview; "All insights" opens the full list with filters.
- Rows with nothing to say are hidden; if all four are empty, show "Nothing needs you right now" with the setup checklist instead.
- The tag top right shows the window (Last 7 days). The footer says where the numbers come from.
- "Wanted nearby" is the seed of TodayZ's demand data for brands: it never names the searching customer.
