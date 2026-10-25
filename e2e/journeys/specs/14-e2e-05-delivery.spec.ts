import { Customer, cartSaved, customerFirstRun, expect, openApp, Shop, test, URLS, type Order } from '../../fixtures/journey';
import { LAKSHMI, PEOPLE, QA_POINT, SL_ITEMS, STORES } from '../../fixtures/seed';

// E2E-05: a delivery order end to end with a rider; the full phone is visible to the shop only while
// the order is out for delivery. Sri Lakshmi delivers within 3 km of the QA point for ₹30 (free on
// ₹500+, minimum ₹150); globalSetup opens it all day, and its delivery hours follow the opening hours.
// The three steps share one order, so they run in order and stop at the first failure.
test.describe.serial('E2E-05 delivery', () => {
  let orderId = '';

  test('customer books delivery to a saved address', async ({ who, browser }) => {
    const c = new Customer(await who('delivery'));
    const home = await c.savedAddress('Home', QA_POINT);
    const app = await openApp(browser, c.who, URLS.customer);
    try {
      const { page } = app;
      await customerFirstRun(page);
      await test.step('C-08: one atta pack goes in the cart (₹245, over the ₹150 minimum)', async () => {
        await page.goto(`/s/${STORES.SL.slug}/i/${SL_ITEMS.atta.id}`);
        await page.getByRole('button', { name: 'Add to cart' }).click();
        await cartSaved(page);
      });
      await test.step('C-09: Delivery shows the terms and quotes the fee to the saved address', async () => {
        await page.goto('/cart');
        await page.getByRole('radio', { name: 'Delivery' }).click();
        await expect(page.getByText(/^₹30 delivery · Minimum ₹150 · Free delivery on ₹500\+/)).toBeVisible();
        await expect(page.getByText(/^Deliver to Home: /)).toBeVisible();
        await expect(page.getByText('Add ₹255 more for free delivery')).toBeVisible();
      });
      await test.step('C-11: the saved address is picked and the order is placed at ₹275', async () => {
        await page.getByRole('button', { name: /Checkout/ }).first().click();
        await expect(page.getByRole('heading', { level: 1, name: 'Checkout' })).toBeVisible();
        await expect(page.getByRole('radio', { name: 'Delivery' })).toHaveAttribute('aria-checked', 'true');
        await expect(page.getByRole('group', { name: 'Deliver to' }).getByRole('radio', { name: /Home/ })).toBeChecked();
        await page.getByRole('button', { name: 'Place order · ₹275' }).click();
        await expect(page.getByTestId('placed-check')).toBeVisible();
        await expect(page).toHaveURL(/\/orders\/[0-9A-Z]{26}$/);
        orderId = new URL(page.url()).pathname.split('/').pop() ?? '';
      });
    } finally {
      await app.close();
    }
    const o = await c.order(orderId);
    expect(o.fulfilment).toBe('delivery');
    expect(o.status).toBe('placed');
    expect(o.deliveryFeePaise).toBe(3000);
    expect(o.totalPaise).toBe(27500);
    expect(o.code, 'no code before the shop accepts').toBeNull();
    expect(home.addressId).toMatch(/^[0-9A-Z]{26}$/);
  });

  test('shop accepts, dispatches with a rider, and the order is delivered', async ({ who, browser }) => {
    expect(orderId, 'the order from the step before').not.toBe('');
    const c = new Customer(await who('delivery'));
    const owner = await who('owner');
    const shop = new Shop(owner, STORES.SL);
    const app = await openApp(browser, owner, URLS.business);
    try {
      const { page } = app;
      await page.goto('/choose-shop');
      await page.getByRole('region', { name: `Stores of ${LAKSHMI.name}` }).getByRole('button', { name: STORES.SL.name, exact: true }).click();
      await page.goto(`/orders/${orderId}`);
      await test.step('the shop accepts in the business app', async () => {
        await page.getByRole('button', { name: 'Accept', exact: true }).click();
        await expect(page.getByRole('button', { name: 'Dispatch' })).toBeVisible();
      });
      expect((await c.order(orderId)).status).toBe('accepted');
      await test.step('the rider takes it: Dispatch → out for delivery, with a Call link', async () => {
        await page.getByRole('button', { name: 'Dispatch' }).click();
        await expect(page.getByRole('heading', { name: "Deliver with the customer's code" })).toBeVisible();
        // The dispatch answer never carries the phone (it may be stored for a replay); the next
        // read of the order does.
        await page.reload();
        await expect(page.getByRole('link', { name: /^Call / })).toBeVisible();
      });
    } finally {
      await app.close();
    }
    const out = await c.order(orderId);
    expect(out.status).toBe('out_for_delivery');

    const customerApp = await openApp(browser, c.who, URLS.customer);
    try {
      await test.step('C-12 shows the delivery code to tell the rider', async () => {
        const { page } = customerApp;
        await customerFirstRun(page);
        await page.goto(`/orders/${orderId}`);
        await expect(page.getByText('Your order from Sri Lakshmi is on the way.')).toBeVisible();
        await expect(page.getByText('Delivery code', { exact: true })).toBeVisible();
        await expect(page.getByText('Tell this code to the rider when your order arrives.')).toBeVisible();
      });
    } finally {
      await customerApp.close();
    }

    await test.step('the rider enters the code: delivered', async () => {
      const done = await shop.handOver(orderId, await c.code(orderId));
      expect(done.status).toBe('delivered');
    });
    const delivered: Order = await c.order(orderId);
    expect(delivered.status).toBe('delivered');
  });

  test('the full phone shows to the shop only while out for delivery', async ({ who }) => {
    const c = new Customer(await who('delivery'));
    const shop = new Shop(await who('owner'), STORES.SL);
    // Compared, never printed: the assertion messages carry the status only.
    const isFull = (phone: string | null) => (phone ?? '').replace(/\D/g, '').endsWith(PEOPLE.delivery.national);

    const items = [{ storeItemId: SL_ITEMS.atta.id, quantity: 1 }];
    const q = await c.api.ok<{ totalPaise: number }>('customer prices delivery', 'POST', '/v1/me/checkout/quote', {
      storeId: STORES.SL.id,
      fulfilment: 'delivery',
      addressId: (await c.savedAddress('Home', QA_POINT)).addressId,
      items,
      creditsToUse: 0,
    });
    const placed = await c.api.ok<Order>('customer places delivery', 'POST', '/v1/me/orders', {
      storeId: STORES.SL.id,
      fulfilment: 'delivery',
      addressId: (await c.savedAddress('Home', QA_POINT)).addressId,
      items,
      creditsToUse: 0,
      expectedTotalPaise: q.totalPaise,
    });

    const placedView = await shop.order(placed.id);
    expect(placedView.customer.phone, 'placed: no full phone').toBeNull();
    expect(placedView.customer.phoneMasked, 'placed: the masked phone').not.toBeNull();

    await shop.accept(placed.id);
    expect((await shop.order(placed.id)).customer.phone, 'accepted: no full phone').toBeNull();

    const dispatched = await shop.dispatch(placed.id);
    expect(dispatched.status).toBe('out_for_delivery');
    expect(dispatched.customer.phone, 'the dispatch answer never carries it (idempotent replay)').toBeNull();
    const out = await shop.order(placed.id);
    expect(out.status).toBe('out_for_delivery');
    expect(isFull(out.customer.phone), 'out for delivery: the full phone').toBe(true);

    await shop.handOver(placed.id, await c.code(placed.id));
    const after = await shop.order(placed.id);
    expect(after.status).toBe('delivered');
    expect(after.customer.phone, 'delivered: no full phone').toBeNull();
  });
});
