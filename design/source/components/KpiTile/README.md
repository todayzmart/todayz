# KpiTile

Today's key numbers for business and admin dashboards, drawn as one strip: a single bordered card split into cells by hairlines (`tz-kpi-strip`, `tz-kpi-cell`). This is quieter than four floating tiles, the way Kargul Studio's UniOps and Salesline dashboards do it.

- Each cell: a small `ink-3` icon and a sentence-case name ("Sales today"), the value in `kpi` (Figtree, tabular figures), one line of context in `ink-2` ("142 bills · 37 online orders").
- The value is `kpi`: 28px at weight 500, not bold, with -0.02em tracking, as in Kargul Studio's dashboards.
- Footer band on `surface-subtle` with a hairline above: the delta and the comparison ("▲ 12%" + "vs last Thursday"), or one link when the number needs action ("Reorder list →").
- Delta: plain text with an arrow, up in `brand`, down in `danger`, flat in `ink-2` (`tz-kpi-delta-flat`); the comparison follows in `ink-2`. Arrows plus colour, never colour alone.
- The lead metric gets the wider first cell and 32px (`tz-kpi-cell-lead`), as Square does with net sales.
- Compare with the same weekday last week by default, because local shops swing by day of week.
- Four cells on desktop, two by two on phone. Never more than four. No coloured icon tiles and no brand-filled hero card: colour is kept for status and the one primary action.
- `tz-kpi` single tiles remain for places with one number, such as a wallet balance or the admin queue counts.
