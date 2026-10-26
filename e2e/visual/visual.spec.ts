import { expect, test } from '@playwright/test';

import { openContext, settle } from '../a11y/harness';
import { ROUTES } from '../a11y/routes';

// One baseline per key screen: phone (390x844) for the customer app, 1280x800 for business, admin and
// the site, plus the phone for business and the site's home. English everywhere, and en-XA (the
// pseudo-locale, `?locale=pseudo`) for the three apps; the site has no pseudo-locale. Times and
// counts that move are masked (elements marked data-volatile, relative times, order numbers).
const PHONE = { width: 390, height: 844 };
const DESKTOP = { width: 1280, height: 800 };

interface Variant {
  size: string;
  viewport: { width: number; height: number };
  pseudo: boolean;
}

function variants(app: string): Variant[] {
  if (app === 'customer') return [
    { size: 'phone', viewport: PHONE, pseudo: false },
    { size: 'phone', viewport: PHONE, pseudo: true },
  ];
  if (app === 'site') return [
    { size: 'phone', viewport: PHONE, pseudo: false },
    { size: 'desktop', viewport: DESKTOP, pseudo: false },
  ];
  return [
    { size: 'desktop', viewport: DESKTOP, pseudo: false },
    { size: 'desktop', viewport: DESKTOP, pseudo: true },
    { size: 'phone', viewport: PHONE, pseudo: false },
  ];
}

const only = process.env['TZ_VISUAL_APP'];

for (const route of ROUTES.filter((r) => r.visual && (!only || r.app === only))) {
  for (const v of variants(route.app)) {
    const locale = v.pseudo ? 'en-XA' : 'en';
    test(`${route.app}-${route.name}-${v.size}-${locale}`, async ({ browser }) => {
      const { context, close } = await openContext(browser, route.app, route.auth, v.viewport);
      try {
        const page = await context.newPage();
        await settle(page, route, v.pseudo);
        await expect(page).toHaveScreenshot(`${route.app}-${route.name}-${v.size}-${locale}.png`, {
          fullPage: false,
          mask: [page.locator('[data-volatile], time, [data-testid="relative-time"]')],
        });
      } finally {
        await close();
      }
    });
  }
}
