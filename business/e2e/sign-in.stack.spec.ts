import { expect, test, type APIRequestContext, type Page } from '@playwright/test';

// Needs `make dev` running. The SMS code is read from the mocks server; reserved test numbers only.
const MOCKS = process.env.TZ_MOCKS_URL ?? 'http://127.0.0.1:4010';

function testPhone(): string {
  // +91 90000 000xx: the reserved block. xx is picked per run so reruns do not hit one number's rate limit.
  const xx = String(10 + Math.floor(Math.random() * 90));
  return `90000000${xx}`;
}

async function latestOtp(request: APIRequestContext, national: string, since: string): Promise<string> {
  const r = await request.get(`${MOCKS}/_mock/sms/latest-otp`, {
    params: { to: `+91${national}`, since, wait: '8' },
  });
  expect(r.status(), 'the mocks server returned a code').toBe(200);
  const body = (await r.json()) as { otp: string };
  return body.otp;
}

async function signIn(page: Page, request: APIRequestContext, phone: string) {
  const since = new Date().toISOString();
  await page.getByLabel('Mobile number').fill(phone);
  await page.getByRole('button', { name: 'Send code' }).click();
  await expect(page.getByText(/We sent a code to/)).toBeVisible();
  const code = await latestOtp(request, phone, since);
  await page.getByRole('group', { name: 'Enter the 6-digit code' }).locator('input').first().click();
  await page.keyboard.type(code);
}

test('sign in, pick a shop if asked, see Overview, stay signed in across a reload, sign out', async ({ page, request }) => {
  const phone = testPhone();

  // Signed out: any page opens B-01 and remembers where the person was going.
  await page.goto('/orders');
  await expect(page).toHaveURL(/\/sign-in\?next=%2Forders$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in to TodayZ Business' })).toBeVisible();
  await signIn(page, request, phone);

  // The picker: with no business yet (or before W1-04 lands) it offers a way on; with exactly one store it skips itself.
  const carryOn = page.getByRole('button', { name: 'Continue to overview' });
  const orders = page.getByRole('heading', { level: 1, name: 'Orders' });
  await expect(carryOn.or(orders).or(page.getByText(/No shop is linked/))).toBeVisible();
  if (await carryOn.isVisible()) await carryOn.click();
  else if (await page.getByText(/No shop is linked/).isVisible()) await page.goto('/orders');
  await expect(orders).toBeVisible();

  // B-03 Overview still works behind the gate.
  await page.getByRole('navigation', { name: 'Business' }).first().getByRole('link', { name: 'Overview' }).click();
  await expect(page.getByRole('heading', { level: 1, name: /^Good / })).toBeVisible();

  // The refresh cookie keeps the session across a reload; the token itself is never in storage.
  await page.reload();
  await expect(page.getByRole('heading', { level: 1, name: /^Good / })).toBeVisible();
  const stored = await page.evaluate(() => JSON.stringify({ ...localStorage }) + JSON.stringify({ ...sessionStorage }));
  expect(stored).not.toMatch(/eyJ/);

  // Sign out, and a reload stays signed out.
  await page.getByRole('button', { name: 'Sign out' }).first().click();
  await expect(page.getByRole('heading', { name: 'Sign in to TodayZ Business' })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Sign in to TodayZ Business' })).toBeVisible();
});

test('a wrong code is refused with a clear message', async ({ page, request }) => {
  await page.goto('/sign-in');
  const phone = testPhone();
  const since = new Date().toISOString();
  await page.getByLabel('Mobile number').fill(phone);
  await page.getByRole('button', { name: 'Send code' }).click();
  await expect(page.getByText(/We sent a code to/)).toBeVisible();
  const real = await latestOtp(request, phone, since);
  // Not 000000: the reserved test numbers accept it locally (01 §2).
  const wrong = real === '123456' ? '654321' : '123456';
  await page.getByRole('group', { name: 'Enter the 6-digit code' }).locator('input').first().click();
  await page.keyboard.type(wrong);
  await expect(page.getByText('That code is not right. Check it and try again.')).toBeVisible();
});
