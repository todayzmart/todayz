# CategoryTile

Shop categories on the customer home and in search filters, each in its own pastel tint.

- Grocery `green`, Pharmacy `rose`, Beauty `lilac`, Electronics `sky`, Stationery `amber`, Hardware `peach`. New categories reuse one of these six tints; never invent a new hue.
- Tile: 56px square, `radius-lg`, `tint-*` fill, 24px Lucide icon in the matching `tint-*-ink`. Label 12px, 500, `ink`, under the tile.
- A shop's letter avatar takes the tint of its main category (`.tz-avatar[data-tint]`), so a pharmacy reads rose and a kirana green across the app.
- Set the tint with `data-tint="green"` etc. on the element; children read `--t-bg` and `--t-ink`.
