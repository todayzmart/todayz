import { expect, test, type APIRequestContext, type Page } from '@playwright/test';

// Needs `make dev` running. The SMS code is read from the mocks server; reserved test numbers only.
// The SMS code alone signs an admin in (D-264). TZ_ADMIN_PHONE (national 10 digits) is a seeded staff
// number for the console journey; without it that test is skipped.
const MOCKS = process.env.TZ_MOCKS_URL ?? 'http://127.0.0.1:4010';

// Each test signs in with its own number: the API limits how often one phone may ask for a code.
function reservedPhone(): string {
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

test('signed out opens sign-in; the SMS code alone opens the console for a seeded admin (D-264)', async ({ page, request }) => {
  const phone = process.env.TZ_ADMIN_PHONE;

  await page.goto('/businesses');
  await expect(page).toHaveURL(/\/sign-in\?next=%2Fbusinesses$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in to TodayZ Admin' })).toBeVisible();

  test.skip(!phone, 'needs TZ_ADMIN_PHONE, a seeded staff number');
  if (!phone) return;
  const since = await sendCode(page, phone);
  await typeInto(page, 'Enter the 6-digit code', await latestOtp(request, phone, since));

  await expect(page.getByRole('navigation', { name: 'Main' })).toBeVisible();
  await expect(page).toHaveURL(/\/businesses$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Two-step check' })).toHaveCount(0);

  // A reload keeps the console open: the rotated refresh cookie signs the tab back in.
  await page.reload();
  await expect(page.getByRole('navigation', { name: 'Main' })).toBeVisible();
  if (process.env.TZ_QA_DIR) await page.screenshot({ path: `${process.env.TZ_QA_DIR}/admin-console-1280.png` });
});

test('a wrong SMS code is refused with a clear message', async ({ page, request }) => {
  await page.goto('/sign-in');
  const phone = reservedPhone();
  const since = await sendCode(page, phone);
  const real = await latestOtp(request, phone, since);
  // Not 000000: the reserved test numbers accept it locally (01 §2).
  await typeInto(page, 'Enter the 6-digit code', real === '123456' ? '654321' : '123456');
  await expect(page.getByText('That code is not right. Check it and try again.')).toBeVisible();
});
