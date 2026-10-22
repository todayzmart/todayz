# Chip

Filter and radius chips in a horizontally scrolling row; `aria-pressed="true"` marks the selected ones.

- Use for search radius (1, 2, 5, 10 km), filters (Open now, Pickup, Delivery, Offers) and POS categories.
- Selected chips are black: `inverse` fill with `on-inverse` text. Single-choice rows behave like radio buttons.
- The consumer provides the label, pressed state and handler; keep labels to two words.
