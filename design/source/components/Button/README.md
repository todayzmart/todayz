# Button

The one control for every action. Use `tz-btn-primary` at most once per view, for the thing the screen is for.

- Variants:
  - Default: white with a `line-control` border and `shadow-xs`, weight 500. The workhorse: Export, Directions, Reject.
  - `tz-btn-soft`: grey fill, no border, for small in-place actions like Change. `tz-btn-soft-danger`: red tint for Remove.
  - `tz-btn-primary`: black `inverse` fill with `on-inverse` label. The main action of a view, such as Accept order, Save or New sale.
  - `tz-btn-brand`: green fill. Used only where money changes hands: the customer's Place order and the POS Charge.
  - `tz-btn-ghost`: `ink-2` text, no border, grey wash on hover. Cancel and View all.
  - `tz-btn-danger`: `danger` fill, destructive only, after confirmation.
- Filled variants carry `shadow-btn`, a soft drop with an inner top highlight, so they feel pressable without a gradient.
- Sizes: `tz-btn-sm` (32px, 13px text, `radius-sm`) for toolbars and table rows, default (36px, `radius-md`), `tz-btn-lg` (48px) for checkout and onboarding. Under `data-density="touch"` the default grows to 44px. Icons inside buttons are 16px with a 6px gap. `tz-btn-block` fills its container; use it for the bottom action on phone screens.
- `tz-btn-icon` makes a square outlined icon-only button (32px with `tz-btn-sm`, 36px default, 44px on touch) with an `ink-2` icon; give it an `aria-label`.
- The consumer provides a verb-first, sentence-case label and an optional leading Lucide icon. Disabled buttons use the `disabled` attribute; explain why nearby.
