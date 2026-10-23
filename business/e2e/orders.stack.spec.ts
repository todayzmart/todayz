import { expect, test, type APIRequestContext, type Page } from '@playwright/test';

// Needs `make dev` running with the seed. Two contexts: the customer (API only, since the customer
// checkout screens come with W2-09) and the shop (the real business app in the browser).
// The pickup code is read from the customer's own order in memory and typed once; it is never logged.
// NOT RUN by the W2-10 weekend slice (no `make dev` there); it is here for the lead's first stack run.
const API = process.env.TZ_API_URL ?? 'https://api.todayz.localhost';
const MOCKS = process.env.TZ_MOCKS_URL ?? 'http://127.0.0.1:4010';
const CUSTOMER_PHONE = process.env.TZ_CUSTOMER_PHONE ?? '9000000001'; // seed: customer.asha
const OWNER_PHONE = process.env.TZ_BIZ_OWNER_PHONE ?? '9000000011'; // seed: Lakshmi Traders owner
const STORE_SLUG = 'sri-lakshmi-indiranagar'; // seed: Sri Lakshmi
const STORE_ID = '01KE0S1W6W8ZV7T1C3A6MS0001';
const STORE_NAME = 'Sri Lakshmi';

const CROCKFORD = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';
function ulid(): string {
  let t = Date.now();
  let time = '';
  for (let i = 0; i < 10; i++) {
    time = CROCKFORD[t % 32] + time;
    t = Math.floor(t / 32);
  }
  let rand = '';
  for (let i = 0; i < 16; i++) rand += CROCKFORD[Math.floor(Math.random() * 32)];
  return time + rand;
}

async function latestOtp(request: APIRequestContext, national: string, since: string): Promise<string> {
  const r = await request.get(`${MOCKS}/_mock/sms/latest-otp`, { params: { to: `+91${national}`, since, wait: '8' } });
  expect(r.status(), 'the mocks server returned a code').toBe(200);
  return ((await r.json()) as { otp: string }).otp;
}

/** Signs the seeded customer in over the API and places a one-item pickup order. Returns what the test needs. */
async function placeOrder(request: APIRequestContext): Promise<{ orderId: string; number: string; token: string }> {
  const since = new Date().toISOString();
  const send = await request.post(`${API}/v1/auth/otp/send`, {
    data: { phone: `+91${CUSTOMER_PHONE}`, purpose: 'login', app: 'customer', turnstileToken: 'XXXX.DUMMY.TOKEN.XXXX' },
  });
  expect(send.status()).toBe(200);
  const { requestId } = (await send.json()) as { requestId: string };
  const code = await latestOtp(request, CUSTOMER_PHONE, since);
  const verify = await request.post(`${API}/v1/auth/otp/verify`, { data: { requestId, code, app: 'customer', deviceId: ulid() } });
  expect(verify.status()).toBe(200);
  const { accessToken } = (await verify.json()) as { accessToken: string };
  const auth = { Authorization: `Bearer ${accessToken}` };

  const items = await request.get(`${API}/v1/discover/stores/${STORE_SLUG}/items`, { params: { limit: 20 } });
  expect(items.status()).toBe(200);
  const list = (await items.json()) as { items: { storeItemId: string; pricePaise: number; inStock: boolean }[] };
  const item = list.items.find((i) => i.inStock);
  expect(item, 'the seeded shop has an item in stock').toBeTruthy();
  if (!item) throw new Error('no item in stock');

  const placed = await request.post(`${API}/v1/me/orders`, {
    headers: { ...auth, 'Idempotency-Key': ulid() },
    data: {
      storeId: STORE_ID,
      fulfilment: 'pickup',
      items: [{ storeItemId: item.storeItemId, quantity: 1 }],
      creditsToUse: 0,
      expectedTotalPaise: item.pricePaise,
    },
  });
  expect(placed.status()).toBe(201);
  const order = (await placed.json()) as { id: string; number: string };
  return { orderId: order.id, number: order.number, token: accessToken };
}

async function shopSignIn(page: Page, request: APIRequestContext) {
  await page.goto('/orders');
  const since = new Date().toISOString();
  await page.getByLabel('Mobile number').fill(OWNER_PHONE);
  await page.getByRole('button', { name: 'Send code' }).click();
  await expect(page.getByText(/We sent a code to/)).toBeVisible();
  const code = await latestOtp(request, OWNER_PHONE, since);
  await page.getByRole('group', { name: 'Enter the 6-digit code' }).locator('input').first().click();
  await page.keyboard.type(code);
  await page.getByRole('region', { name: 'Stores of Lakshmi Traders' }).getByRole('button', { name: STORE_NAME, exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Orders' })).toBeVisible();
}

test('a customer order goes new, accepted, ready and handed over with the pickup code', async ({ page, request }) => {
  const { orderId, number, token } = await placeOrder(request);

  await shopSignIn(page, request);

  // Live: the new order shows on the New tab without a reload, with a countdown and its stock.
  const card = page.getByRole('article', { name: number });
  await expect(card).toBeVisible({ timeout: 20_000 });
  await expect(card.getByTestId('countdown')).toBeVisible();
  if (process.env.TZ_QA_DIR) await page.screenshot({ path: `${process.env.TZ_QA_DIR}/business-orders-new-1280.png` });

  // Accept it; it leaves New and appears under Accepted.
  await card.getByRole('button', { name: `Accept order ${number}` }).click();
  await expect(card).toBeHidden();
  await page.getByRole('tab', { name: 'Accepted' }).click();
  await page.getByRole('article', { name: number }).getByRole('link', { name: `View order ${number}` }).click();
  await expect(page.getByRole('heading', { level: 1, name: `Order ${number}` })).toBeVisible();

  // Mark ready.
  await page.getByRole('button', { name: 'Mark ready' }).click();
  await expect(page.getByText('Ready for pickup').first()).toBeVisible();

  // A wrong code first: it says how many tries are left, and the boxes clear.
  const customerOrder = await request.get(`${API}/v1/me/orders/${orderId}`, { headers: { Authorization: `Bearer ${token}` } });
  expect(customerOrder.status()).toBe(200);
  const code = ((await customerOrder.json()) as { code: string | null }).code;
  expect(code, 'the customer sees the code once the order is accepted').toMatch(/^\d{4}$/);
  const wrong = code === '0000' ? '1111' : '0000';
  const boxes = page.getByRole('group', { name: "Customer's 4-digit code" }).locator('input');
  await boxes.first().click();
  await page.keyboard.type(wrong);
  await page.getByRole('radio', { name: 'Cash' }).click();
  await page.getByRole('button', { name: 'Hand over order' }).click();
  await expect(page.getByText(/That code is not right\. 4 tries left\./)).toBeVisible();

  // The right code, paid in cash, hands the order over.
  await boxes.first().click();
  await page.keyboard.type(code ?? '');
  await page.getByRole('button', { name: 'Hand over order' }).click();
  await expect(page.getByText('Order handed over.')).toBeVisible();

  // The customer's order is collected and records the payment method.
  const after = await request.get(`${API}/v1/me/orders/${orderId}`, { headers: { Authorization: `Bearer ${token}` } });
  const done = (await after.json()) as { status: string; paymentMethod: string | null };
  expect(done.status).toBe('collected');
  expect(done.paymentMethod).toBe('cash');
});

test('an owner can cancel an accepted order; the reason "other" needs a note', async ({ page, request }) => {
  const { number } = await placeOrder(request);
  await shopSignIn(page, request);
  const card = page.getByRole('article', { name: number });
  await card.getByRole('button', { name: `Accept order ${number}` }).click();
  await page.getByRole('tab', { name: 'Accepted' }).click();
  await page.getByRole('article', { name: number }).getByRole('link', { name: `View order ${number}` }).click();

  await page.getByRole('button', { name: 'Cancel order' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('radio', { name: 'Other reason' }).click();
  await expect(dialog.getByRole('button', { name: 'Cancel order' })).toBeDisabled();
  await dialog.getByRole('textbox').fill('Power cut at the shop');
  await dialog.getByRole('button', { name: 'Cancel order' }).click();
  await expect(page.getByText('Order cancelled.')).toBeVisible();
});
