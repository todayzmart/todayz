import { createHmac } from 'node:crypto';

import { expect, test, type APIRequestContext, type Page } from '@playwright/test';

// Needs `make dev` running. The SMS code is read from the mocks server; reserved test numbers only.
// The admin phone must be a seeded staff number for the OTP step to succeed (TZ_ADMIN_PHONE, national
// 10 digits); without it the run uses a reserved number and stops at the first screen that needs staff.
const MOCKS = process.env.TZ_MOCKS_URL ?? 'http://127.0.0.1:4010';

// Each test signs in with its own number: the API limits how often one phone may ask for a code.
function reservedPhone(): string {
  const xx = String(10 + Math.floor(Math.random() * 90));
  return `90000000${xx}`;
}

function testPhone(): string {
  return process.env.TZ_ADMIN_PHONE ?? reservedPhone();
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

/** RFC 6238 (SHA-1, 6 digits, 30 s) from a base32 secret: the seeded admins' test-only secrets (e2e/fixtures/admins.yaml). */
function totp(secret: string, at = Date.now()): string {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  let bits = '';
  for (const ch of secret.replace(/=+$/, '').toUpperCase()) bits += alphabet.indexOf(ch).toString(2).padStart(5, '0');
  const key = Buffer.from(bits.match(/.{8}/g)?.map((b) => parseInt(b, 2)) ?? []);
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(at / 1000 / 30)));
  const mac = createHmac('sha1', key).update(counter).digest();
  const offset = (mac[mac.length - 1] ?? 0) & 0x0f;
  const n = (mac.readUInt32BE(offset) & 0x7fffffff) % 1_000_000;
  return String(n).padStart(6, '0');
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
  const phone = reservedPhone();
  const since = await sendCode(page, phone);
  const real = await latestOtp(request, phone, since);
  // Not 000000: the reserved test numbers accept it locally (01 §2).
  await typeInto(page, 'Enter the 6-digit code', real === '123456' ? '654321' : '123456');
  await expect(page.getByText('That code is not right. Check it and try again.')).toBeVisible();
});

// Needs TZ_ADMIN_CONSOLE_PHONE and TZ_ADMIN_TOTP_SECRET for an enrolled seeded admin other than
// TZ_ADMIN_PHONE's (e2e/fixtures/admins.yaml).
test('a seeded admin with the right authenticator code reaches the console', async ({ page, request }) => {
  const phone = process.env.TZ_ADMIN_CONSOLE_PHONE;
  const secret = process.env.TZ_ADMIN_TOTP_SECRET;
  test.skip(!phone || !secret, 'needs an enrolled seeded admin');
  if (!phone || !secret) return;

  await page.goto('/businesses');
  const since = await sendCode(page, phone);
  await typeInto(page, 'Enter the 6-digit code', await latestOtp(request, phone, since));
  await expect(page.getByRole('group', { name: 'Authenticator code' })).toBeVisible();

  // Not in the last seconds of a 30 s window, so the code is still current when it arrives.
  const left = 30 - (Math.floor(Date.now() / 1000) % 30);
  if (left < 5) await page.waitForTimeout(left * 1000 + 500);
  await typeInto(page, 'Authenticator code', totp(secret));

  await expect(page.getByRole('navigation', { name: 'Main' })).toBeVisible();
  await expect(page).toHaveURL(/\/businesses$/);

  // A reload keeps the console open: the rotated refresh cookie signs the tab back in.
  await page.reload();
  await expect(page.getByRole('navigation', { name: 'Main' })).toBeVisible();
  if (process.env.TZ_QA_DIR) await page.screenshot({ path: `${process.env.TZ_QA_DIR}/admin-console-1280.png` });
});
