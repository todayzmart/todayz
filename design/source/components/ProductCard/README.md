# ProductCard

A product as one shop sells it: image, name, unit, stock with freshness, price with MRP, and Add.

- Two per row on phone (`tz-grid2`), 4–6 per row on desktop storefronts. Names clamp to two lines.
- Show MRP and the discount only when the shop's price is lower.
- In cross-shop search results, add the shop name and distance as a `tz-meta` line under the unit.
- Out of stock cards keep their place, dim the image and replace Add with "Notify me".
- The consumer provides image URL or initials, name, unit, price, MRP, stock state and timestamp, and cart quantity.
