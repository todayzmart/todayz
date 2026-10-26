import { mkdirSync, rmSync } from 'node:fs';
import { join } from 'node:path';

import type { Browser, BrowserContext, Page } from '@playwright/test';

import { principal, type Principal } from '../fixtures/principals';
import { PEOPLE, QA_POINT, STORES, URLS } from '../fixtures/seed';
import type { AppName, RouteDef, Who } from './routes';

// Opens a route the way a person would see it: a context per (app, person), the principal's own
// session lent to it, English, the seed's QA point as the customer's place and Sri Lakshmi as the
// shop the business app remembers. Nothing here prints a token, phone or code.

export const ORIGINS: Record<AppName, string> = {
  customer: URLS.customer,
  business: URLS.business,
  admin: URLS.admin,
  site: process.env['TZ_SITE_URL'] ?? 'https://todayz.localhost',
};

export async function openContext(
  browser: Browser,
  app: AppName,
  who: Who,
  viewport: { width: number; height: number },
  lang = 'en',
): Promise<{ context: BrowserContext; close(): Promise<void> }> {
  const context = await browser.newContext({
    baseURL: ORIGINS[app],
    ignoreHTTPSErrors: true,
    locale: 'en-IN',
    timezoneId: 'Asia/Kolkata',
    viewport,
    reducedMotion: 'reduce',
    serviceWorkers: 'block',
  });
  let p: Principal | undefined;
  if (who) {
    p = await principal(PEOPLE[who]);
    await p.attach(context);
  }
  await context.addInitScript(
    ({ app: a, place, store, lang: l }) => {
      try {
        localStorage.setItem('todayz.lang', l);
        if (a === 'customer') localStorage.setItem('todayz.place', place);
        if (a === 'business') localStorage.setItem('todayz.biz.store', store);
        if (a === 'admin') sessionStorage.setItem('todayz.admin.2fa', String(Date.now()));
      } catch {
        // storage blocked: the screen shows what it shows
      }
    },
    { app, place: JSON.stringify({ kind: 'gps', lat: QA_POINT.lat, lng: QA_POINT.lng }), store: STORES.SL.id, lang },
  );
  return {
    context,
    close: async () => {
      if (p) await p.detach();
      await context.close();
    },
  };
}

/** Goes to the route and waits until the screen has settled (network idle, fonts, no skeletons). */
export async function settle(page: Page, route: RouteDef, pseudo = false): Promise<void> {
  const path = pseudo ? `${route.path}${route.path.includes('?') ? '&' : '?'}locale=pseudo` : route.path;
  await page.goto(path, { waitUntil: 'load' });
  // Screens with a live stream (orders, tracking) never go idle: do not wait for them past 5 s.
  await page.waitForLoadState('networkidle', { timeout: 5000 }).catch(() => undefined);
  await page.evaluate(() => document.fonts.ready);
  // Skeleton and loading states are not the screen: wait for them to go (best effort, 6 s).
  await page
    .waitForFunction(() => !document.querySelector('[aria-busy="true"], .tz-skeleton, [data-loading="true"]'), undefined, { timeout: 6000 })
    .catch(() => undefined);
  await page.waitForTimeout(400);
}

export function resultsDir(...parts: string[]): string {
  const dir = join(import.meta.dirname, 'out', ...parts);
  mkdirSync(dir, { recursive: true });
  return dir;
}

export function resetResults(file: string): void {
  rmSync(join(import.meta.dirname, 'out', file), { force: true });
}
