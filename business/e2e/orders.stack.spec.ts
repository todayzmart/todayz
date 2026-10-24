import { expect, request as playwrightRequest, test, type APIRequestContext, type Page } from '@playwright/test';

// Needs `make dev` running with the seed. Two contexts: the customer (API only, since the customer
// checkout screens come with W2-09) and the shop (the real business app in the browser).
// The pickup code is read from the customer's own order in memory and typed once; it is never logged.
// The seed manager opens Sri Lakshmi all day for the run (the API refuses orders outside opening
// hours) and puts the seed's hours back afterwards. OTP sends are kept low: 30 s apart per number,
// 5 per number and 20 per address an hour; a send inside the 30 s waits it out.
const API = process.env.TZ_API_URL ?? 'https://api.todayz.localhost';
const MOCKS = process.env.TZ_MOCKS_URL ?? 'http://127.0.0.1:4010';
const CUSTOMER_PHONE = process.env.TZ_CUSTOMER_PHONE ?? '9000000001'; // seed: customer.asha
const OWNER_PHONE = process.env.TZ_BIZ_OWNER_PHONE ?? '9000000011'; // seed: Lakshmi Traders owner
const MANAGER_PHONE = '9000000012'; // seed: Lakshmi Traders manager (Sri Lakshmi)
const BUSINESS_ID = '01KE0S1W6W8ZV7T1C3A6MB0001'; // seed: Lakshmi Traders
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

/** POST /v1/auth/otp/send, waiting out the 30 s resend gap once (a run straight after another hits it). */
async function sendOtp(request: APIRequestContext, data: Record<string, string>) {
  const r = await request.post(`${API}/v1/auth/otp/send`, { data });
  const wait = Number(r.headers()['retry-after'] ?? '0');
  if (r.status() !== 429 || wait > 35) return r;
  await new Promise((done) => setTimeout(done, wait * 1000 + 500));
  return request.post(`${API}/v1/auth/otp/send`, { data });
}

/** Signs in over the API (one OTP send) and returns the access token. */
async function apiSignIn(request: APIRequestContext, national: string, app: 'customer' | 'business'): Promise<string> {
  const since = new Date().toISOString();
  const send = await sendOtp(request, { phone: `+91${national}`, purpose: 'login', app, turnstileToken: 'XXXX.DUMMY.TOKEN.XXXX' });
  expect(send.status()).toBe(200);
  const { requestId } = (await send.json()) as { requestId: string };
  const code = await latestOtp(request, national, since);
  const verify = await request.post(`${API}/v1/auth/otp/verify`, { data: { requestId, code, app, deviceId: ulid() } });
  expect(verify.status()).toBe(200);
  return ((await verify.json()) as { accessToken: string }).accessToken;
}

let customerToken: string | undefined;

// OTP resend gaps can add half a minute.
test.describe.configure({ timeout: 120_000 });

const ALL_DAY = {
  weekly: Object.fromEntries(['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'].map((d) => [d, [{ open: '00:00', close: '00:00' }]])),
  holidays: [],
};
let restoreHours: (() => Promise<void>) | undefined;
let managerAuth: Record<string, string> | undefined;

test.beforeAll(async () => {
  const request = await playwrightRequest.newContext({ ignoreHTTPSErrors: true });
  const auth = { Authorization: `Bearer ${await apiSignIn(request, MANAGER_PHONE, 'business')}` };
  managerAuth = auth;
  const url = `${API}/v1/biz/businesses/${BUSINESS_ID}/stores/${STORE_ID}/settings`;
  const got = await request.get(url, { headers: auth });
  expect(got.status(), 'read the store settings').toBe(200);
  const before = ((await got.json()) as { hours: unknown }).hours;
  const set = async (hours: unknown, etag: string) => {
    const r = await request.patch(url, { headers: { ...auth, 'If-Match': etag }, data: { hours } });
    expect(r.status(), `changed the store hours: ${await r.text()}`).toBe(200);
    return r.headers()['etag'] ?? '';
  };
  const etag = await set(ALL_DAY, got.headers()['etag'] ?? '');
  restoreHours = async () => {
    await set(before, etag);
    await request.dispose();
  };
});

test.afterAll(async () => {
  await restoreHours?.();
});

/** Signs the seeded customer in over the API (once per run) and places a one-item pickup order. Returns what the test needs. */
async function placeOrder(request: APIRequestContext, quantity = 1): Promise<{ orderId: string; number: string; token: string; storeItemId: string }> {
  customerToken ??= await apiSignIn(request, CUSTOMER_PHONE, 'customer');
  const accessToken = customerToken;
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
      items: [{ storeItemId: item.storeItemId, quantity }],
      creditsToUse: 0,
      expectedTotalPaise: item.pricePaise * quantity,
    },
  });
  expect(placed.status()).toBe(201);
  const order = (await placed.json()) as { id: string; number: string };
  return { orderId: order.id, number: order.number, token: accessToken, storeItemId: item.storeItemId };
}

async function shopSignIn(page: Page, request: APIRequestContext) {
  await page.goto('/orders');
  let since = new Date().toISOString();
  await page.getByLabel('Mobile number').fill(OWNER_PHONE);
  await page.getByRole('button', { name: 'Send code' }).click();
  // The number may have had a code less than 30 s ago (the previous test or spec): the app counts down, then we send again.
  const sent = page.getByText(/We sent a code to/);
  const tooMany = page.getByText('Too many tries. Wait a moment and try again.');
  await expect(sent.or(tooMany)).toBeVisible();
  if (await tooMany.isVisible()) {
    const send = page.getByRole('button', { name: /^(Send|Resend) code$/ });
    await expect(send).toBeEnabled({ timeout: 40_000 });
    since = new Date().toISOString();
    await send.click();
  }
  await expect(sent).toBeVisible();
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
  await expect(card.getByText(/\d+ in stock$/).first()).toBeVisible();
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
  await expect(page.getByText('Marked ready.')).toBeVisible();
  if (process.env.TZ_QA_DIR) await page.screenshot({ path: `${process.env.TZ_QA_DIR}/business-order-ready-1280.png` });

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
  if (process.env.TZ_QA_DIR) await page.screenshot({ path: `${process.env.TZ_QA_DIR}/business-order-handed-1280.png` });

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

test('Accept is disabled while a line is short; the shop rejects the order instead', async ({ page, request }) => {
  const { number, storeItemId } = await placeOrder(request, 2);
  // The manager counts the item down to 1 (fewer than the 2 asked for), and puts the count back afterwards.
  const itemUrl = `${API}/v1/biz/businesses/${BUSINESS_ID}/stores/${STORE_ID}/items/${storeItemId}`;
  const count = async (quantity: number) => {
    const r = await request.post(`${itemUrl}/movements`, {
      headers: { ...managerAuth, 'Idempotency-Key': ulid() },
      data: { kind: 'count_correction', quantity },
    });
    expect(r.status(), `counted the stock: ${await r.text()}`).toBe(201);
  };
  const before = await request.get(itemUrl, { headers: { ...managerAuth } });
  expect(before.status()).toBe(200);
  const onHand = ((await before.json()) as { onHand: number }).onHand;
  await count(1);
  try {
    await shopSignIn(page, request);
    const card = page.getByRole('article', { name: number });
    await expect(card.getByText(/^Not enough stock for one item\./)).toBeVisible({ timeout: 20_000 });
    await expect(card.getByRole('button', { name: `Accept order ${number}` })).toBeDisabled();
    if (process.env.TZ_QA_DIR) await page.screenshot({ path: `${process.env.TZ_QA_DIR}/business-order-short-1280.png` });

    await card.getByRole('button', { name: `Reject order ${number}` }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByRole('radio', { name: 'Out of stock' }).click();
    await dialog.getByRole('button', { name: 'Reject order' }).click();
    await expect(card).toBeHidden();
  } finally {
    await count(onHand);
  }
});
