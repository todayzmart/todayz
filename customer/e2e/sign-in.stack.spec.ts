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

test('first run: language, location, sign in, stay signed in across a reload, sign out', async ({ page, request }) => {
  const phone = testPhone();

  // C-01: the first visit asks for a language once.
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: 'Choose your language' })).toBeVisible();
  await page.getByRole('button', { name: 'English' }).click();
  await page.getByRole('button', { name: 'Continue' }).click();

  // C-02: a PIN code.
  await expect(page.getByRole('heading', { level: 1, name: 'Where should we look?' })).toBeVisible();
  await page.getByRole('textbox', { name: 'PIN code' }).fill('560001');
  await page.getByRole('button', { name: 'Use this PIN code' }).click();
  await expect(page.getByRole('link', { name: /Shopping near 560001/ })).toBeVisible();

  // C-10: Account asks to sign in; the code comes from the mocks.
  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Account' }).click();
  await page.getByRole('link', { name: 'Sign in' }).click();
  await signIn(page, request, phone);

  // A brand-new number gets the optional name step; an existing one goes straight to Account.
  const skip = page.getByRole('button', { name: 'Skip for now' });
  const masked = page.getByText(/•/);
  await expect(skip.or(masked)).toBeVisible();
  if (await skip.isVisible()) {
    await page.getByLabel('Your name (optional)').fill('Test Customer');
    await page.getByRole('button', { name: 'Continue' }).click();
  }
  await expect(page.getByRole('heading', { level: 1, name: 'Account' })).toBeVisible();
  await expect(masked).toBeVisible();

  // The refresh cookie keeps the session across a reload; the token itself is never in storage.
  await page.reload();
  await expect(page.getByText(/•/)).toBeVisible();
  const stored = await page.evaluate(() => JSON.stringify({ ...localStorage }) + JSON.stringify({ ...sessionStorage }));
  expect(stored).not.toMatch(/eyJ/);

  // Sign out, and a reload stays signed out.
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page.getByRole('heading', { name: 'Sign in to TodayZ' })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Sign in to TodayZ' })).toBeVisible();
});

test('a wrong code is refused with a clear message', async ({ page, request }) => {
  await page.addInitScript(() => localStorage.setItem('todayz.lang', 'en'));
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
