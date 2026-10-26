// D-249 bright skin: phone screenshots of each restyled screen, on the e2e build with a faked API (no stack).
// Runs only with TZ_QA_DIR, e.g. `TZ_QA_DIR=docs/qa/D-249 pnpm exec playwright test e2e/bright.shots.spec.ts`.
import { shot } from '@todayz/config/playwright';
import { test, type Page, type Route } from '@playwright/test';

test.skip(!process.env['TZ_QA_DIR'], 'set TZ_QA_DIR to refresh the screenshots');

const PHONE = { width: 360, height: 780 } as const;
const USER = '01J9ZAAAAAAAAAAAAAAAAAAAAA';
const NOW = new Date().toISOString();

const cors = (route: Route) => {
  const req = route.request();
  return {
    'access-control-allow-origin': req.headers()['origin'] ?? '*',
    'access-control-allow-credentials': 'true',
    'access-control-allow-headers': req.headers()['access-control-request-headers'] ?? '*',
    'access-control-allow-methods': 'GET, POST, PUT, PATCH, DELETE, OPTIONS',
  };
};
const ok = (route: Route, body: unknown, headers: Record<string, string> = {}) =>
  route.fulfill({ status: 200, contentType: 'application/json', headers: { ...cors(route), ...headers }, body: JSON.stringify(body) });

const addresses = [
  { addressId: '01J9ZADDRAAAAAAAAAAAAAAAA1', label: 'Home', line1: '12, 4th Cross', line2: 'Indiranagar', landmark: 'Near the park', pincode: '560038', lat: 12.97, lng: 77.64, isDefault: true, updatedAt: NOW, version: 1 },
  { addressId: '01J9ZADDRAAAAAAAAAAAAAAAA2', label: 'Office', line1: '3rd floor, 80 Feet Road', line2: 'Koramangala', landmark: null, pincode: '560034', lat: 12.93, lng: 77.62, isDefault: false, updatedAt: NOW, version: 1 },
];

async function signedIn(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem('todayz.lang', 'en');
    localStorage.setItem('todayz.hasSession.customer', '1');
    localStorage.setItem('todayz.place', JSON.stringify({ kind: 'pin', pin: '560038' }));
  });
  await page.route('**/v1/**', (route) => {
    const req = route.request();
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers: cors(route) });
    const p = new URL(req.url()).pathname;
    if (p === '/v1/auth/refresh')
      return ok(route, { accessToken: 'tok', tokenType: 'Bearer', expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), userId: USER });
    if (p === '/v1/me') return ok(route, { id: USER, phoneMasked: '+91 ••••• •0021', name: 'Asha', preferredLang: 'en' }, { etag: '"1"' });
    if (p === '/v1/me/addresses') return ok(route, { addresses });
    if (p === '/v1/me/stream') return route.fulfill({ status: 200, contentType: 'text/event-stream', headers: cors(route), body: '' });
    return route.fulfill({ status: 404, headers: cors(route), body: '' });
  });
}

test('account', async ({ page }) => {
  await page.setViewportSize(PHONE);
  await signedIn(page);
  await page.goto('/account');
  await page.getByRole('button', { name: 'Sign out' }).waitFor();
  await page.getByText('Office').waitFor();
  await page.evaluate(() => document.fonts.ready);
  await shot(page, 'account.png');
  await shot(page, 'account-full.png', { fullPage: true });
});
