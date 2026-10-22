# OrderTimeline

Every status an order passes through, time-stamped, identical on the customer and shop sides.

- Done steps get a filled `brand` dot with a check; the current step a `brand` ring; upcoming steps are hollow with `ink-2` text.
- Pickup orders: Placed, Accepted, Preparing, Ready for pickup, Collected. Delivery orders swap the last two for Out for delivery, Delivered.
- Rejected or cancelled orders end the list with a `danger` step and the reason.
