// W4-05 QA evidence: main screens in one language. Runs only with TZ_LANG (en|hi|kn|te|ta) and TZ_QA_DIR, e.g.
// `TZ_LANG=hi TZ_QA_DIR=docs/qa/W4-05/screens/admin/hi pnpm exec playwright test e2e/i18n.shots.spec.ts`.
import { VIEWPORTS, shot } from '@todayz/config/playwright';
import { test } from '@playwright/test';
import { answerEmptyLists, answerMe, answerRefresh } from './fake-api';

const LANG = process.env['TZ_LANG'] ?? '';
test.skip(!LANG || !process.env['TZ_QA_DIR'], 'set TZ_LANG and TZ_QA_DIR to refresh the screenshots');

const SCREENS: [string, string][] = [
  ['pulse', '/'],
  ['customers', '/customers'],
  ['orders', '/orders'],
  ['settings', '/settings'],
  ['support', '/support'],
];

test.beforeEach(async ({ page }) => {
  await answerEmptyLists(page);
  await answerRefresh(page, true);
  await answerMe(page, ['super_admin']);
  await page.addInitScript((lang) => {
    localStorage.setItem('todayz.lang', lang);
    localStorage.setItem('todayz.hasSession.admin', '1');
    sessionStorage.setItem('todayz.admin.2fa', String(Date.now()));
  }, LANG);
});

for (const [name, path] of SCREENS) {
  test(`${name} in ${LANG}`, async ({ page }) => {
    await page.setViewportSize(VIEWPORTS.desktop);
    await page.goto(path);
    await page.waitForLoadState('networkidle');
    await page.evaluate(() => document.fonts.ready);
    await shot(page, `${name}.png`);
  });
}
