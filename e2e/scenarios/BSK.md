# Basket scenarios (SC-BSK)

Owner: W2-14 (`backend/internal/baskets`, `backend/internal/search/basket*.go`, `customer/src/basket`). BSK-01…BSK-15 are fixed in [08-testing.md](../../docs/plan/08-testing.md); these add to them. Format as in 08 §3: `ID · Given / When / Then · level` (U unit, I integration, E E2E, M manual).

| ID | Scenario | Lv | Where |
|---|---|---|---|
| BSK-16 | Given the customer's list of saved baskets, when they delete one, then it leaves the list at once and a toast offers "Undo" for 8 s; Undo brings it back and no `DELETE` is sent; without Undo the `DELETE` is sent after 8 s, or at once when they leave the page | U | `customer/src/test/basket.test.tsx` |
| BSK-17 | Given a saved basket open in two places, when one edit autosaves with a stale `If-Match`, then the `412` shows "This basket changed on another device. Showing the latest." and the basket is read again, so the screen shows the stored version | U | `customer/src/test/basket.test.tsx` |
| BSK-18 | Given a signed-in customer on a shop page (C-07), a search result (C-05) or a product (C-08), when they tap "Add to basket" and pick a saved basket, then the basket is read fresh and written back with `If-Match` (one retry on `412`), an existing line goes up by one, and a toast links to the basket; signed out, the line goes into the basket on this device | U, E | `customer/src/test/basket.test.tsx`, `customer/e2e/basket.stack.spec.ts` |
| BSK-19 | Given a split where the first shop's order was placed, when its tracking page (C-12) shows a failed status (rejected, expired, cancelled, delivery failed), then "Next: the rest from {shop}" is not shown; "Not now" forgets the pending split | U | `customer/src/test/basket.test.tsx` |
| BSK-20 | Given a device basket and a customer who has just signed in, when they open their baskets, then a banner offers to save it; Save creates it with its lines and clears the device copy; "Not now" hides the offer for this device | U | `customer/src/test/basket.test.tsx` |
| BSK-21 | Given the seed after `make reset`, when Asha opens "Weekly groceries" and searches, then Sri Lakshmi shows "Has everything" and Fill cart lands on the cart for that shop; "Sunday breakfast" shows "Get it all from two shops" with Sri Lakshmi first | E | `customer/e2e/basket.stack.spec.ts` |
