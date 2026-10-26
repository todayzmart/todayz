// W4-05 QA evidence: the site's main pages in one language (the language is the path prefix). Runs only with TZ_LANG
// (en|hi|kn|te|ta) and TZ_QA_DIR, e.g. `TZ_LANG=hi TZ_QA_DIR=docs/qa/W4-05/screens/site/hi pnpm exec playwright test e2e/i18n.shots.spec.ts`.
import { shot } from '@todayz/config/playwright';
import { test } from '@playwright/test';
import { blockTurnstile } from './helpers.ts';

const LANG = process.env['TZ_LANG'] ?? '';
test.skip(!LANG || !process.env['TZ_QA_DIR'], 'set TZ_LANG and TZ_QA_DIR to refresh the screenshots');

const PAGES: [string, string][] = [
  ['home', '/'],
  ['for-retailers', '/for-retailers/'],
];
// Only home and for-retailers exist in hi, kn, te and ta (src/lib/pages.ts); the other pages are English only.

for (const [name, path] of PAGES) {
  test(`${name} in ${LANG}`, async ({ page }) => {
    await blockTurnstile(page);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.goto(LANG === 'en' ? path : `/${LANG}${path}`);
    await page.evaluate(() => document.fonts.ready);
    await shot(page, `${name}.png`);
  });
}
