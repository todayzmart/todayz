# QuantityStepper

The Add button that becomes a − 1 + stepper in place once the item is in the cart.

- Start as `tz-add` ("Add"). After the first tap, swap to `tz-stepper` in the same footprint so the layout does not jump.
- At zero, return to "Add". At the available stock, disable + and show "Only 3 left" below.
- Buttons need `aria-label`s ("Remove one", "Add one"); the count is announced politely.
