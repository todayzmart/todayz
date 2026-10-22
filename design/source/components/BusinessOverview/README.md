# BusinessOverview

The business app's home on desktop: what happened today, what needs doing now, and what is selling.

- Light sidebar with a mint `wash-strong` active item and a `brand` icon, counts on items, and a setup-progress card at the bottom.
- Top bar: global search with ⌘K, notifications, and the primary action "New sale".
- The greeting sits in a mint `tz-greet` band (wash-strong fading to white) with the range control (Today, 7 days, 30 days) and the comparison ("vs last Thursday") on its right.
- Today's numbers sit in one KPI strip (see KpiTile): sales is the wider lead cell with a faint mint wash, each cell has a context line and a footer band with the comparison. The top bar shows the sync state ("Synced 6:40 pm").
- Sales by hour: today's bars, the current hour solid `brand` and earlier hours light grey; last week as a dotted `ink-3` line; black `inverse` tooltip.
- New orders show a countdown to accept and half-width Reject and Accept buttons.
- Demand insights (see DemandInsights) turns sales, stock and nearby searches into four tinted rows, each with one action. Top items show the source of the last stock change (POS, Online, Voice).
- On phone the same blocks stack in this order: KPI strip (two by two), new orders, demand insights, chart, top items.
