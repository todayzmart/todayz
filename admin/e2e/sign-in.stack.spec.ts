import { expect, test, type APIRequestContext, type Page } from '@playwright/test';

// Needs `make dev` running. The SMS code is read from the mocks server; reserved test numbers only.
// The admin phone must be a seeded staff number for the OTP step to succeed (TZ_ADMIN_PHONE, national
// 10 digits); without it the run uses a reserved number and stops at the first screen that needs staff.
const MOCKS = process.env.TZ_MOCKS_URL ?? 'http://127.0.0.1:4010';

function testPhone(): string {
  if (process.env.TZ_ADMIN_PHONE) return process.env.TZ_ADMIN_PHONE;
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

async function sendCode(page: Page, phone: string) {
  const since = new Date().toISOString();
  await page.getByLabel('Mobile number').fill(phone);
  await page.getByRole('button', { name: 'Send code' }).click();
  await expect(page.getByText(/We sent a code to/)).toBeVisible();
  return since;
}

async function typeInto(page: Page, group: string, code: string) {
  await page.getByRole('group', { name: group }).locator('input').first().click();
  await page.keyboard.type(code);
}

test('signed out opens sign-in, the SMS code leads to the two-step check', async ({ page, request }) => {
  const phone = testPhone();

  await page.goto('/businesses');
  await expect(page).toHaveURL(/\/sign-in\?next=%2Fbusinesses$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in to TodayZ Admin' })).toBeVisible();

  const since = await sendCode(page, phone);
  await typeInto(page, 'Enter the 6-digit code', await latestOtp(request, phone, since));

  // Step 2. Until W1-01b is merged the TOTP calls answer 404 and the screen says so; once merged it
  // shows the setup key (first sign-in) or the code boxes (returning). Either way the console stays closed.
  await expect(page.getByRole('heading', { level: 1, name: 'Two-step check' })).toBeVisible();
  await expect(page.getByRole('navigation', { name: 'Main' })).toHaveCount(0);

  const key = page.getByTestId('totp-secret');
  const unavailable = page.getByText(/two-step check is not available yet/);
  const codeBoxes = page.getByRole('group', { name: 'Authenticator code' });
  await expect(key.or(unavailable).or(codeBoxes)).toBeVisible();

  // A wrong authenticator code is refused and the console stays closed.
  if (await codeBoxes.isVisible()) {
    await typeInto(page, 'Authenticator code', '000000');
    await expect(page.getByText(/That code is not right/)).toBeVisible();
    await expect(page.getByRole('navigation', { name: 'Main' })).toHaveCount(0);
  }
});

test('a wrong SMS code is refused with a clear message', async ({ page, request }) => {
  await page.goto('/sign-in');
  const phone = testPhone();
  const since = await sendCode(page, phone);
  const real = await latestOtp(request, phone, since);
  // Not 000000: the reserved test numbers accept it locally (01 §2).
  await typeInto(page, 'Enter the 6-digit code', real === '123456' ? '654321' : '123456');
  await expect(page.getByText('That code is not right. Check it and try again.')).toBeVisible();
});
