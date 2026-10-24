import { test } from '../../fixtures/journey';

// E2E-05: a delivery order end to end with a rider; the full phone is visible to the shop only while
// the order is out for delivery.
test.describe('E2E-05 delivery', () => {
  test.fixme('customer books delivery to a saved address', async () => {
    // Deferred: there is no customer address API (createOrder needs addressId) and C-11 is pickup only.
  });
  test.fixme('shop accepts, dispatches with a rider, and the order is delivered', async () => {
    // Deferred: depends on the step above; the business app's dispatch UI is not built.
  });
  test.fixme('the full phone shows to the shop only while out for delivery', async () => {
    // Deferred: StoreOrder carries customer.phoneMasked only; the full-phone reveal is not built.
  });
});
