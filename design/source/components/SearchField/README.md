# SearchField

The pill-shaped search that leads the customer home screen and the POS product grid.

- Structure: `tz-search` wrapping a search icon, an `<input type="search">` and an optional trailing icon button (scan barcode in the POS, voice in the customer app).
- Placeholder names real things: "Search atta, milk, medicines…" for customers, "Search by name or scan barcode" in the POS.
- The consumer provides the input's `id`, the label (visually hidden is fine) and the handler. Results update as the person types after 2 characters, with a 200ms debounce.
