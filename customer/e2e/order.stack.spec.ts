import { expect, test, type APIRequestContext, type Page } from '@playwright/test';

// W2-09 stack journey. Needs `make dev` with the seed loaded. NOT run by the author of W2-09 (the
// brief forbids running the stack); the lead runs it:
//   pnpm --filter @todayz/customer exec playwright test -c playwright.stack.config.ts order.stack.spec.ts
//
// Sign in as a customer, put a seed item in the cart, check out, see the shop's answer arrive live,
// read the pickup code, then let the shop verify it with that code and see the order collected.
//
// TODO(lead): the shop side needs a seed shop owner and the ids of the seed business and store.
// They are read from the environment (names only, no values here):
//   TZ_SHOP_PHONE        a reserved test number (+91 90000 000xx block) that owns the seed store
//   TZ_SHOP_BUSINESS_ID  the seed business id
//   TZ_SHOP_STORE_ID     the seed store id
//   TZ_SHOP_STORE_SLUG   the seed store slug (the page the customer shops from)
// The shop's token is fetched through the same OTP route the business app uses, with the code read
// from the mocks server. Nothing is printed.
const MOCKS = process.env.TZ_MOCKS_URL ?? 'http://127.0.0.1:4010';
const API = process.env.TZ_API_URL ?? 'https://api.todayz.localhost';
const BIZ = process.env.TZ_SHOP_BUSINESS_ID ?? '';
const STORE = process.env.TZ_SHOP_STORE_ID ?? '';
const SLUG = process.env.TZ_SHOP_STORE_SLUG ?? '';
const SHOP_PHONE = process.env.TZ_SHOP_PHONE ?? '';

const testPhone = () => `90000000${String(10 + Math.floor(Math.random() * 90))}`;

async function latestOtp(request: APIRequestContext, national: string, since: string): Promise<string> {
  const r = await request.get(`${MOCKS}/_mock/sms/latest-otp`, { params: { to: `+91${national}`, since, wait: '8' } });
  expect(r.status(), 'the mocks server returned a code').toBe(200);
  return ((await r.json()) as { otp: string }).otp;
}

async function customerSignIn(page: Page, request: APIRequestContext, phone: string) {
  const since = new Date().toISOString();
  await page.getByLabel('Mobile number').fill(phone);
  await page.getByRole('button', { name: 'Send code' }).click();
  await expect(page.getByText(/We sent a code to/)).toBeVisible();
  const code = await latestOtp(request, phone, since);
  await page.getByRole('group', { name: 'Enter the 6-digit code' }).locator('input').first().click();
  await page.keyboard.type(code);
  const skip = page.getByRole('button', { name: 'Skip for now' });
  if (await skip.isVisible().catch(() => false)) await skip.click();
}

/** The shop owner's access token, kept in this test's memory only. */
async function shopToken(request: APIRequestContext): Promise<string> {
  const since = new Date().toISOString();
  const send = await request.post(`${API}/v1/auth/otp/send`, {
    data: { phone: SHOP_PHONE, purpose: 'login', app: 'business', turnstileToken: 'dev' },
  });
  expect(send.ok(), 'shop OTP was sent').toBe(true);
  const { requestId } = (await send.json()) as { requestId: string };
  const otp = await latestOtp(request, SHOP_PHONE, since);
  const verify = await request.post(`${API}/v1/auth/otp/verify`, {
    data: { requestId, code: otp, app: 'business', deviceId: 'e2e-shop' },
  });
  expect(verify.ok(), 'shop signed in').toBe(true);
  return ((await verify.json()) as { accessToken: string }).accessToken;
}

test('book from the seed shop, shop accepts, the code shows, the shop verifies, collected', async ({ page, request }) => {
  test.skip(!BIZ || !STORE || !SLUG || !SHOP_PHONE, 'needs TZ_SHOP_* ids from the seed (see the header)');
  await page.addInitScript(() => localStorage.setItem('todayz.lang', 'en'));

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
  await page.getByRole('button', { name: /^Place order/ }).click();

  // C-12: the 400 ms check shows once, then we wait for the shop with a countdown from the server.
  await expect(page.getByTestId('placed-check')).toBeVisible();
  await expect(page).toHaveURL(/\/orders\/[0-9A-Z]{26}$/);
  await expect(page.getByText(/Waiting for .* to accept · \d+:\d{2} left/)).toBeVisible();
  const orderId = page.url().split('/').pop() ?? '';

  // The shop accepts through the API; the customer's page moves on its own (SSE, no reload).
  const token = await shopToken(request);
  const auth = { Authorization: `Bearer ${token}` };
  const base = `${API}/v1/biz/businesses/${BIZ}/stores/${STORE}/orders/${orderId}`;
  const accept = await request.post(`${base}/accept`, { headers: { ...auth, 'Idempotency-Key': `e2e-accept-${orderId}` }, data: {} });
  expect(accept.ok(), 'shop accepted').toBe(true);

  const group = page.getByRole('group', { name: 'Pickup code' });
  await expect(group).toBeVisible({ timeout: 15_000 });
  const code = ((await group.locator('.tz-code-digits [aria-hidden="true"]').textContent()) ?? '').trim();
  expect(code).toMatch(/^\d{4}$/);
  await expect(page.getByText(/Held for you · \d+:\d{2} left/)).toBeVisible();

  // The code is never in the URL or in browser storage.
  expect(page.url()).not.toContain(code);
  const stored = await page.evaluate(() => JSON.stringify({ ...localStorage }) + JSON.stringify({ ...sessionStorage }));
  expect(stored).not.toContain(code);

  // The shop verifies the code at the counter; the customer sees the order collected.
  const verify = await request.post(`${base}/verify-code`, {
    headers: { ...auth, 'Idempotency-Key': `e2e-verify-${orderId}` },
    data: { code, paymentMethod: 'cash' },
  });
  expect(verify.ok(), 'shop verified the code').toBe(true);
  await expect(page.getByText('Order complete')).toBeVisible({ timeout: 15_000 });
  await expect(page.getByRole('group', { name: 'Pickup code' })).toHaveCount(0);

  // It shows under Earlier in the orders list.
  await page.goto('/orders');
  await expect(page.getByRole('region', { name: 'Earlier' }).getByText('Collected')).toBeVisible();
});
