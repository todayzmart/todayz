// QA evidence: full-page screenshots of every page at 390 and 1440 px, plus home in every language,
// with reduced motion so the CSS animations rest on their final frame.
// Only runs when TZ_QA_DIR is set, e.g. `TZ_QA_DIR=docs/qa/W1-09/screens pnpm exec playwright test e2e/screens.spec.ts`.
import { guardPage, shot } from '@todayz/config/playwright';
import { expect, test } from '@playwright/test';
import { blockTurnstile } from './helpers.ts';

test.skip(!process.env['TZ_QA_DIR'], 'set TZ_QA_DIR to refresh the screenshots');

const SIZES = { phone: { width: 390, height: 844 }, desktop: { width: 1440, height: 900 } };

const PAGES: [string, string][] = [
  ['home', '/'],
  ['home-hi', '/hi/'],
  ['home-kn', '/kn/'],
  ['home-te', '/te/'],
  ['home-ta', '/ta/'],
  ['for-retailers', '/for-retailers/'],
  ['for-retailers-hi', '/hi/for-retailers/'],
  ['how-it-works', '/how-it-works/'],
  ['pricing', '/pricing/'],
  ['about', '/about/'],
  ['contact', '/contact/'],
  ['privacy', '/legal/privacy/'],
  ['terms-for-shoppers', '/legal/terms-for-shoppers/'],
  ['terms-for-shops', '/legal/terms-for-shops/'],
  ['refunds-and-credits', '/legal/refunds-and-credits/'],
  ['404', '/no-such-page'],
];

for (const [size, viewport] of Object.entries(SIZES)) {
  for (const [name, path] of PAGES) {
    test(`${name} at ${size}`, async ({ page }) => {
      const guard = await guardPage(page);
      await blockTurnstile(page);
      await page.setViewportSize(viewport);
      await page.emulateMedia({ reducedMotion: 'reduce' });
      await page.goto(path);
      await page.evaluate(() => document.fonts.ready);
      await shot(page, `${name}-${size === 'phone' ? '390' : '1440'}.png`, { fullPage: true });
      expect(guard.external).toEqual([]);
    });
  }
}
