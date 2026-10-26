// W4-05 QA evidence: main screens in one language on a phone. Runs only with TZ_LANG (en|hi|kn|te|ta) and TZ_QA_DIR, e.g.
// `TZ_LANG=hi TZ_QA_DIR=docs/qa/W4-05/screens/customer/hi pnpm exec playwright test e2e/i18n.shots.spec.ts`.
import { VIEWPORTS, shot } from '@todayz/config/playwright';
import { test } from '@playwright/test';

const LANG = process.env['TZ_LANG'] ?? '';
test.skip(!LANG || !process.env['TZ_QA_DIR'], 'set TZ_LANG and TZ_QA_DIR to refresh the screenshots');

const SCREENS: [string, string][] = [
  ['home', '/'],
  ['search', '/search'],
  ['cart', '/cart'],
  ['sign-in', '/sign-in'],
  ['help', '/help'],
  ['language', '/language'],
];

test.beforeEach(async ({ page }) => {
  await page.addInitScript((lang) => localStorage.setItem('todayz.lang', lang), LANG);
  await page.route('**/v1/auth/refresh', (route) =>
    route.fulfill({
      status: 401,
      contentType: 'application/problem+json',
      headers: { 'access-control-allow-origin': '*' },
      body: JSON.stringify({ type: 'about:blank#unauthorized', title: 'Unauthorized', status: 401, code: 'unauthorized' }),
    }),
  );
});

for (const [name, path] of SCREENS) {
  test(`${name} in ${LANG}`, async ({ page }) => {
    await page.setViewportSize(VIEWPORTS.phone);
    await page.goto(path);
    await page.waitForLoadState('networkidle');
    await page.evaluate(() => document.fonts.ready);
    await shot(page, `${name}.png`);
  });
}
