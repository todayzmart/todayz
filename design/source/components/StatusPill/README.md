# StatusPill

The fixed vocabulary of order and payment states, shown identically to customer, shop and admin. Every pill is neutral: 22px tall, white with a hairline `line` border, `ink` text at weight 500, `radius-xs`. The state lives in the coloured dot, so a table full of orders stays calm.

| State | Class | Colour |
| --- | --- | --- |
| Pending | `tz-pill-pending` | `warning` dot |
| Accepted | `tz-pill-accepted` | `info` dot |
| Preparing | `tz-pill-preparing` | `teal` dot |
| Ready for pickup | `tz-pill-ready` | `brand` dot and text on `brand-soft` with a faint green border (the only tinted pill) |
| Out for delivery | `tz-pill-delivery` | `info` dot |
| Completed | `tz-pill-completed` | `brand` dot |
| Rejected, Cancelled | `tz-pill-rejected`, `tz-pill-cancelled` | `danger` dot |
| Unpaid | `tz-pill` | `ink-3` dot |
| Paid | `tz-pill-paid` | `brand` dot |

- Order status and payment status are separate pills; show both on order cards.
- Always the dot and the word. Never shorten "Ready for pickup" to "Ready" on the customer side.
