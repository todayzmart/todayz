import { test } from '../../fixtures/journey';

// E2E-19 (builder-written): an admin blocks a customer → the customer's sessions end, sign-in and
// ordering are refused, and unblocking restores them.
test.fixme('E2E-19 blocked customer cannot sign in or order', async () => {
  // Deferred: there is no admin customer-block API or screen at this gate (A-xx customers come with
  // W3-05); the seed has no blocked customer to test against either.
});
