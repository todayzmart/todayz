# PosCart

The right-hand cart of the counter POS: customer, lines, discount, payment method and the Charge bar.

- Customer lookup by phone at the top; walk-in is the default.
- Each `tz-pos-line`: name with unit, a stepper, line total. Tap a line to change price or remove.
- Payment method chips: Cash, UPI, Card, Credit (khata); the selected one is black. The `tz-pos-total` bar on `inverse` holds the total in `display`. The Charge button is `tz-btn-brand`, one of the few green buttons in the system, because charging is the moment money changes hands. Steppers are black.
- Recording the sale reduces stock immediately and shows a Toast.
