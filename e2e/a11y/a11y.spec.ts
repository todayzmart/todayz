import { appendFileSync } from 'node:fs';
import { join } from 'node:path';

import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';

import { openContext, resetResults, resultsDir, settle } from './harness';
import { ROUTES, VIEWPORTS_A11Y, type RouteDef } from './routes';

// axe (WCAG 2.2 AA) on every route, three viewports, English. Findings are also appended to
// out/findings.jsonl (route, viewport, rule, impact, node target) for the QA report.
// Also, at every viewport: no sideways scroll (WCAG 1.4.10 reflow; axe cannot see it), and the
// phone viewport again in hi, kn, te and ta for the routes the apps have in those languages
// (long words and wide scripts are where layouts break).
const WCAG = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];
const only = process.env['TZ_A11Y_APP'];
const LANGS = ['hi', 'kn', 'te', 'ta'];

test.beforeAll(() => resetResults('findings.jsonl'));

async function check(page: Page, route: RouteDef, tag: string) {
  const where = `${route.app}${route.path}`;
  const lang = await page.evaluate(() => document.documentElement.lang);
  const { violations } = await new AxeBuilder({ page }).withTags(WCAG).analyze();
  const found = violations.flatMap((v) =>
    v.nodes.map((n) => ({ route: where, viewport: tag, rule: v.id, impact: v.impact, target: n.target.join(' '), html: n.html.slice(0, 160) })),
  );
  if (!lang) found.push({ route: where, viewport: tag, rule: 'html-lang-missing', impact: 'serious', target: 'html', html: '' });
  const over = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  if (over > 0) found.push({ route: where, viewport: tag, rule: 'horizontal-scroll', impact: 'serious', target: `page is ${over}px wider than the screen`, html: '' });
  for (const f of found) appendFileSync(join(resultsDir(), 'findings.jsonl'), `${JSON.stringify(f)}\n`);
  expect(found.map((f) => `${f.rule}: ${f.target}`)).toEqual([]);
}

for (const [vpName, viewport] of Object.entries(VIEWPORTS_A11Y)) {
  for (const route of ROUTES.filter((r) => !only || r.app === only)) {
    test(`axe ${route.app} ${route.name} (${route.path}) @ ${vpName}`, async ({ browser }) => {
      const { context, close } = await openContext(browser, route.app, route.auth, viewport);
      try {
        const page = await context.newPage();
        await settle(page, route);
        await check(page, route, vpName);
      } finally {
        await close();
      }
    });
  }
}

for (const lang of LANGS) {
  // The site's own pages are per language (home-hi, ...), so only the three apps are repeated here.
  for (const route of ROUTES.filter((r) => r.app !== 'site' && (!only || r.app === only))) {
    test(`axe ${route.app} ${route.name} (${route.path}) @ phone-360 ${lang}`, async ({ browser }) => {
      const { context, close } = await openContext(browser, route.app, route.auth, VIEWPORTS_A11Y['phone-360'], lang);
      try {
        const page = await context.newPage();
        await settle(page, route);
        await check(page, route, `phone-360-${lang}`);
      } finally {
        await close();
      }
    });
  }
}
