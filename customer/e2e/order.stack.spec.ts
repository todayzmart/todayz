import { expect, test, type APIRequestContext, type Page } from '@playwright/test';

// W2-09 stack journey. Needs `make dev` with the seed loaded. NOT run by the author of W2-09 (the
// brief forbids running the stack); the lead runs it:
//   pnpm --filter @todayz/customer exec playwright test -c playwright.stack.config.ts order.stack.spec.ts
//
// Sign in as a customer, put a seed item in the cart, check out, see the shop's answer arrive live,
// read the pickup code, then let the shop verify it with that code and see the order collected.
//
// The shop side is the seed's Lakshmi Traders owner on Sri Lakshmi (ids below; each can be overridden
// from the environment). The shop's token comes through the same OTP route the business app uses,
// with the code read from the mocks server. Nothing is printed.
const MOCKS = process.env.TZ_MOCKS_URL ?? 'http://127.0.0.1:4010';
const API = process.env.TZ_API_URL ?? 'https://api.todayz.localhost';
const BIZ = process.env.TZ_SHOP_BUSINESS_ID ?? '01KE0S1W6W8ZV7T1C3A6MB0001'; // seed: Lakshmi Traders
const STORE = process.env.TZ_SHOP_STORE_ID ?? '01KE0S1W6W8ZV7T1C3A6MS0001'; // seed: Sri Lakshmi
const SLUG = process.env.TZ_SHOP_STORE_SLUG ?? 'sri-lakshmi-indiranagar';
const SHOP_PHONE = process.env.TZ_SHOP_PHONE ?? '9000000011'; // national number of the seed owner

// A new customer each run, outside the seed's numbers (01–03, 11–13, 91–92) in the reserved test block.
const testPhone = () => `90000000${String(30 + Math.floor(Math.random() * 60))}`;
const TURNSTILE_DUMMY = 'XXXX.DUMMY.TOKEN.XXXX';

/** Saves a screenshot for the QA pack when TZ_QA_DIR is set (synthetic seed data only). */
async function shot(page: Page, name: string) {
  if (process.env.TZ_QA_DIR) await page.screenshot({ path: `${process.env.TZ_QA_DIR}/${name}-390.png` });
}

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

/** Signs in on /sign-in and waits until the app has finished signing in (a new number may get the name step). */
async function customerSignIn(page: Page, request: APIRequestContext, phone: string) {
  const since = new Date().toISOString();
  await page.getByLabel('Mobile number').fill(phone);
  await page.getByRole('button', { name: 'Send code' }).click();
  await expect(page.getByText(/We sent a code to/)).toBeVisible();
  const code = await latestOtp(request, phone, since);
  await page.getByRole('group', { name: 'Enter the 6-digit code' }).locator('input').first().click();
  await page.keyboard.type(code);
  const skip = page.getByRole('button', { name: 'Skip for now' });
  await page.waitForFunction(
    () => !location.pathname.startsWith('/sign-in') || [...document.querySelectorAll('button')].some((b) => b.textContent?.trim() === 'Skip for now'),
  );
  if (await skip.isVisible()) await skip.click();
  await expect(page).not.toHaveURL(/\/sign-in/);
}

/** POST /v1/auth/otp/send, waiting out the 30 s resend gap once (a run straight after another hits it). */
async function sendOtp(request: APIRequestContext, data: Record<string, string>) {
  const r = await request.post(`${API}/v1/auth/otp/send`, { data });
  const wait = Number(r.headers()['retry-after'] ?? '0');
  if (r.status() !== 429 || wait > 35) return r;
  await new Promise((done) => setTimeout(done, wait * 1000 + 500));
  return request.post(`${API}/v1/auth/otp/send`, { data });
}

/** The shop owner's access token, kept in this test's memory only. */
async function shopToken(request: APIRequestContext): Promise<string> {
  const since = new Date().toISOString();
  const send = await sendOtp(request, { phone: `+91${SHOP_PHONE}`, purpose: 'login', app: 'business', turnstileToken: TURNSTILE_DUMMY });
  expect(send.status(), 'shop OTP was sent').toBe(200);
  const { requestId } = (await send.json()) as { requestId: string };
  const otp = await latestOtp(request, SHOP_PHONE, since);
  const verify = await request.post(`${API}/v1/auth/otp/verify`, {
    data: { requestId, code: otp, app: 'business', deviceId: ulid() },
  });
  expect(verify.status(), 'shop signed in').toBe(200);
  return ((await verify.json()) as { accessToken: string }).accessToken;
}

const ALL_DAY = {
  weekly: Object.fromEntries(['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'].map((d) => [d, [{ open: '00:00', close: '00:00' }]])),
  holidays: [],
};

/**
 * Opens the store all day, since the API refuses orders outside opening hours and the stack may run
 * at night. Returns a function that puts the seed's hours back.
 */
async function openAllDay(request: APIRequestContext, token: string): Promise<() => Promise<void>> {
  const url = `${API}/v1/biz/businesses/${BIZ}/stores/${STORE}/settings`;
  const auth = { Authorization: `Bearer ${token}` };
  const got = await request.get(url, { headers: auth });
  expect(got.status(), 'read the store settings').toBe(200);
  const before = ((await got.json()) as { hours: unknown }).hours;
  const set = async (hours: unknown, etag: string) => {
    const r = await request.patch(url, { headers: { ...auth, 'If-Match': etag }, data: { hours } });
    expect(r.status(), `changed the store hours: ${await r.text()}`).toBe(200);
    return r.headers()['etag'] ?? '';
  };
  const etag = await set(ALL_DAY, got.headers()['etag'] ?? '');
  return async () => {
    await set(before, etag);
  };
}

// OTP resend gaps can add half a minute.
test.describe.configure({ timeout: 120_000 });

test('book from the seed shop, shop accepts, the code shows, the shop verifies, collected', async ({ page, request }) => {
  await page.addInitScript(() => localStorage.setItem('todayz.lang', 'en'));
  const token = await shopToken(request);
  const restoreHours = await openAllDay(request, token);
  try {
    await journey(page, request, token);
  } finally {
    await restoreHours();
  }
});

async function journey(page: Page, request: APIRequestContext, token: string) {
  // Sign in first, then shop (the cart lives on the device and survives sign-in either way).
  await page.goto('/sign-in?next=%2Fcart');
  await customerSignIn(page, request, testPhone());

  // Put the first available seed item in the cart from the shop page (W2-08).
  await page.goto(`/s/${SLUG}`);
  await page.getByRole('button', { name: /^Add/ }).first().click();
  await page.goto('/cart');
  await expect(page.getByRole('heading', { level: 1, name: 'Cart' })).toBeVisible();
  await page.getByRole('button', { name: /Checkout/ }).click();

  // C-11 pickup checkout, then Place order.
  await expect(page.getByRole('heading', { level: 1, name: 'Checkout' })).toBeVisible();
  await shot(page, 'customer-checkout');
  await page.getByRole('button', { name: /^Place order/ }).click();

  // C-12: the 400 ms check shows once, then we wait for the shop with a countdown from the server.
  await expect(page.getByTestId('placed-check')).toBeVisible();
  await expect(page).toHaveURL(/\/orders\/[0-9A-Z]{26}$/);
  await expect(page.getByText(/Waiting for .* to accept · \d+:\d{2} left/)).toBeVisible();
  await shot(page, 'customer-waiting');
  const orderId = page.url().split('/').pop() ?? '';

  // The shop accepts through the API; the customer's page moves on its own (SSE, no reload).
  const auth = { Authorization: `Bearer ${token}` };
  const base = `${API}/v1/biz/businesses/${BIZ}/stores/${STORE}/orders/${orderId}`;
  const accept = await request.post(`${base}/accept`, { headers: { ...auth, 'Idempotency-Key': ulid() }, data: {} });
  expect(accept.ok(), 'shop accepted').toBe(true);

  const group = page.getByRole('group', { name: 'Pickup code' });
  await expect(group).toBeVisible({ timeout: 15_000 });
  const code = ((await group.locator('.tz-code-digits [aria-hidden="true"]').textContent()) ?? '').trim();
  expect(code).toMatch(/^\d{4}$/);
  await expect(page.getByText(/Held for you · \d+:\d{2} left/)).toBeVisible();
  await shot(page, 'customer-pickup-code');

  // The code is never in the URL or in browser storage.
  expect(page.url()).not.toContain(code);
  const stored = await page.evaluate(() => JSON.stringify({ ...localStorage }) + JSON.stringify({ ...sessionStorage }));
  expect(stored).not.toContain(code);

  // The shop verifies the code at the counter; the customer sees the order collected.
  const verify = await request.post(`${base}/verify-code`, {
    headers: { ...auth, 'Idempotency-Key': ulid() },
    data: { code, paymentMethod: 'cash' },
  });
  expect(verify.ok(), 'shop verified the code').toBe(true);
  await expect(page.getByText('Order complete')).toBeVisible({ timeout: 15_000 });
  await expect(page.getByRole('group', { name: 'Pickup code' })).toHaveCount(0);
  await shot(page, 'customer-collected');

  // It shows under Earlier in the orders list.
  await page.goto('/orders');
  await expect(page.getByRole('region', { name: 'Earlier' }).getByText('Collected')).toBeVisible();
}
