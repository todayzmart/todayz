import { guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page, type Route } from '@playwright/test';
import { readFileSync } from 'node:fs';

// C-18 profile photo (D-259) at 360 px on the e2e build with a faked API (no stack): the initial,
// adding a photo (register, PUT to storage with only Content-Type, confirm), the photo on Account
// and in the Home header, a refused file, and removing it from the badge's sheet.
// `TZ_QA_DIR=docs/qa/D-259 pnpm exec playwright test profile-photo.spec.ts` refreshes the committed shots.

const PHONE = { width: 360, height: 780 } as const;
const USER = '01J9ZAAAAAAAAAAAAAAAAAAAAA';
const UPLOAD = '01J9ZBBBBBBBBBBBBBBBBBBBBB';
const PUT_URL = `https://s3.todayz.localhost/todayz-private/profile/${USER}/${UPLOAD}.upload?X-Amz-Signature=put`;
const GET_URL = `https://s3.todayz.localhost/todayz-private/profile/${USER}/${UPLOAD}.jpg?X-Amz-Signature=get`;
const PORTRAIT = readFileSync(new URL('./fixtures/profile-photo.jpg', import.meta.url));

const cors = (route: Route) => {
  const req = route.request();
  return {
    'access-control-allow-origin': req.headers()['origin'] ?? '*',
    'access-control-allow-credentials': 'true',
    'access-control-allow-headers': req.headers()['access-control-request-headers'] ?? '*',
    'access-control-allow-methods': 'GET, POST, PUT, PATCH, DELETE, OPTIONS',
  };
};
const ok = (route: Route, body: unknown, status = 200, headers: Record<string, string> = {}) =>
  route.fulfill({ status, contentType: 'application/json', headers: { ...cors(route), ...headers }, body: JSON.stringify(body) });

async function fakeApi(page: Page) {
  let photoUrl: string | null = null;
  let version = 1;
  const puts: Record<string, string>[] = [];
  const me = () => ({ id: USER, phoneMasked: '+91 ••••• •0021', name: 'Asha', preferredLang: 'en', app: 'customer', createdAt: '2026-10-01T00:00:00Z', photoUrl });

  await page.route('https://s3.todayz.localhost/**', (route) => {
    const req = route.request();
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors(route) });
    if (req.method() === 'PUT') {
      puts.push(req.headers());
      return route.fulfill({ status: 200, headers: cors(route), body: '' });
    }
    return route.fulfill({ status: 200, headers: cors(route), contentType: 'image/jpeg', body: PORTRAIT });
  });
  await page.route('**/v1/**', (route) => {
    const req = route.request();
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors(route) });
    const p = new URL(req.url()).pathname;
    if (p === '/v1/auth/refresh')
      return ok(route, { accessToken: 'tok', tokenType: 'Bearer', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), userId: USER });
    if (p === '/v1/me') return ok(route, me(), 200, { etag: `"${version}"` });
    if (p === '/v1/me/addresses') return ok(route, { addresses: [] });
    if (p === '/v1/me/wallet') return ok(route, { availableCredits: 120, pendingCredits: 0, expiringSoon: [] });
    if (p === '/v1/me/stream') return route.fulfill({ status: 200, contentType: 'text/event-stream', headers: cors(route), body: '' });
    if (p === '/v1/me/photo/uploads' && req.method() === 'POST') {
      const b = req.postDataJSON() as { contentType: string; sizeBytes: number };
      return ok(
        route,
        { id: UPLOAD, upload: { url: PUT_URL, method: 'PUT', expiresAt: new Date(Date.now() + 900_000).toISOString(), headers: { 'Content-Type': b.contentType, 'Content-Length': String(b.sizeBytes) } } },
        201,
        { 'cache-control': 'no-store' },
      );
    }
    if (p === `/v1/me/photo/uploads/${UPLOAD}/confirm`) {
      photoUrl = GET_URL;
      version += 1;
      return ok(route, me(), 200, { etag: `"${version}"` });
    }
    if (p === '/v1/me/photo' && req.method() === 'DELETE') {
      photoUrl = null;
      version += 1;
      return ok(route, me(), 200, { etag: `"${version}"` });
    }
    return route.fulfill({ status: 404, contentType: 'application/problem+json', headers: cors(route), body: JSON.stringify({ type: 'about:blank#not_found', title: 'not_found', status: 404, code: 'not_found' }) });
  });
  return { puts };
}

let guard: PageGuard;
test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  await page.setViewportSize(PHONE);
  await page.addInitScript(() => localStorage.setItem('todayz.lang', 'en'));
  await page.addInitScript(() => localStorage.setItem('todayz.hasSession.customer', '1'));
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  // The faked API answers 404 for screens this test does not open (Home's discover calls).
  expect(guard.errors.filter((e) => !e.includes('status of 404')), 'console and page errors').toEqual([]);
});

test('360: add, show and remove a profile photo (C-18, D-259)', async ({ page }) => {
  const api = await fakeApi(page);
  await page.goto('/account');
  const card = page.getByRole('region', { name: 'Asha' });
  await expect(card.getByText('+91 ••••• •0021')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Add a profile photo' })).toBeVisible();
  await shot(page, 'customer-c18-photo-initial-360.png');

  // A GIF is refused before anything is sent.
  await page.locator('input[type=file]').setInputFiles({ name: 'a.gif', mimeType: 'image/gif', buffer: Buffer.from('GIF89a') });
  await expect(page.getByRole('alert')).toHaveText('Choose a JPEG, PNG or WebP photo.');
  await shot(page, 'customer-c18-photo-refused-360.png');

  await page.locator('input[type=file]').setInputFiles({ name: 'me.jpg', mimeType: 'image/jpeg', buffer: PORTRAIT });
  await expect(page.getByRole('button', { name: 'Change profile photo' })).toBeVisible();
  await expect(card.locator('img')).toHaveAttribute('src', GET_URL);
  await expect(page.getByRole('button', { name: 'Remove photo' })).toHaveCount(0);
  expect(api.puts).toHaveLength(1);
  expect(api.puts[0]?.['content-type']).toBe('image/jpeg');
  await shot(page, 'customer-c18-photo-set-360.png');

  await page.goto('/');
  await expect(page.locator('header a[href="/account"] img')).toHaveAttribute('src', GET_URL);
  await shot(page, 'customer-c03-header-photo-360.png');

  // With a photo, the badge opens a sheet: choose a new one or remove it.
  await page.goto('/account');
  await page.getByRole('button', { name: 'Change profile photo' }).click();
  const sheet = page.getByRole('dialog', { name: 'Profile photo' });
  await expect(sheet.getByRole('button', { name: 'Choose a new photo' })).toBeVisible();
  await shot(page, 'customer-c18-photo-sheet-360.png');
  await sheet.getByRole('button', { name: 'Remove photo' }).click();
  await expect(page.getByRole('button', { name: 'Add a profile photo' })).toBeVisible();
  await expect(card.locator('img')).toHaveCount(0);
  await expect(card.getByText('A', { exact: true })).toBeVisible();
});
