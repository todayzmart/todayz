# DataTable

Dense, sortable lists for stock, orders, customers and admin records.

- Header row 36px, sentence case, 13px `ink-2` at weight 500, on `surface-subtle`; rows 44px with `line` dividers and a `surface-subtle` hover; numbers right-aligned with tabular figures.
- First column carries the name with a `tz-sub` second line (SKU, phone, area).
- Wrap in `tz-table-wrap` so wide tables scroll inside their card on phone.
- Row click opens a detail drawer; bulk actions appear in a bar when rows are selected.
