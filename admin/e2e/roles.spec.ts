import { VIEWPORTS, guardPage, type PageGuard } from '@todayz/config/playwright';
import { expect, test } from '@playwright/test';
import { answerWith, problem, signIn } from './fake-api';

/**
 * AD-02 (08-testing.md): signed in as each of the 7 roles, the sidebar shows exactly that role's
 * screens; two roles see the union; a hidden screen opened by URL shows the no-access state; and a
 * normal flow (opening every screen in the sidebar) never meets a 403.
 *
 * Both tables are written out here from the plan, not imported from the app, so a wrong entry in
 * `src/app/nav/access.ts` fails this test instead of agreeing with it.
 */

/** 06-screens.md §3 "Admin roles and navigation", as sidebar labels in NAV order. */
const ALL = [
  'Pulse',
  'Businesses',
  'Stores',
  'Customers',
  'Orders',
  'Support',
  'Reviews',
  'Banner moderation',
  'Credits and referrals',
  'Master catalogue',
  'Demand',
  'Payouts',
  'Admin users',
  'Settings',
  'Audit log',
  'Notifications',
  'Jobs',
  'System health',
];

const CASES: { roles: string[]; nav: string[]; hidden?: string }[] = [
  // A-03 Leads tab and A-23 Gaps; A-04 and A-15 are not in this sidebar yet (W3-06).
  { roles: ['field_agent'], nav: ['Businesses', 'Demand'], hidden: '/orders' },
  {
    roles: ['support'],
    nav: ['Pulse', 'Businesses', 'Stores', 'Customers', 'Orders', 'Support', 'Reviews', 'Banner moderation', 'Jobs'],
    hidden: '/payouts',
  },
  { roles: ['catalogue'], nav: ['Pulse', 'Master catalogue', 'Demand'], hidden: '/orders' },
  { roles: ['analyst'], nav: ['Pulse', 'Demand', 'Jobs'], hidden: '/settings' },
  { roles: ['finance'], nav: ['Pulse', 'Credits and referrals', 'Payouts', 'Jobs'], hidden: '/customers' },
  { roles: ['admin'], nav: ALL.filter((n) => n !== 'Admin users'), hidden: '/admin-users' },
  { roles: ['super_admin'], nav: ALL },
  { roles: ['catalogue', 'finance'], nav: ['Pulse', 'Credits and referrals', 'Master catalogue', 'Demand', 'Payouts', 'Jobs'], hidden: '/stores' },
];

/**
 * Who the API lets read each admin path, from the policy goldens
 * (backend/internal/platform/authz/policy/testdata) and the A-21 gate (jobs/adminapi/gate.go).
 * The fake answers 403 to everyone else, and to any admin path missing here.
 */
const SUPPORT_UP = ['support', 'admin', 'super_admin'];
const READS: [RegExp, string[]][] = [
  [/^\/v1\/admin\/leads(\/|$)/, ['field_agent', ...SUPPORT_UP]],
  [/^\/v1\/admin\/(businesses|stores|customers|customer-search|orders|reviews|review-reports|banners)(\/|$)/, SUPPORT_UP],
  [/^\/v1\/admin\/audit-log\/subject$/, ['field_agent', 'support', 'catalogue', 'finance', 'admin', 'super_admin']],
  [/^\/v1\/admin\/(audit-log|settings)(\/|$)/, ['admin', 'super_admin']],
  [/^\/v1\/admin\/(jobs|job-runs)(\/|$)/, ['support', 'analyst', 'finance', 'admin', 'super_admin']],
  [/^\/v1\/admin\/(products|pending-products|categories|synonyms)(\/|$)/, ['catalogue', 'admin', 'super_admin']],
  [/^\/v1\/admin\/users(\/|$)/, ['super_admin']],
];

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  await page.setViewportSize(VIEWPORTS.desktop);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
});

for (const c of CASES) {
  test(`${c.roles.join(' + ')}: the sidebar lists exactly its screens, none answers 403`, async ({ page }) => {
    await signIn(page, c.roles);
    await answerWith(page, '/v1/admin/**', (_req, url) => {
      const grant = READS.find(([re]) => re.test(url.pathname))?.[1];
      // `undefined` passes the call to the empty-list answer registered by signIn.
      return grant?.some((r) => c.roles.includes(r)) ? undefined : { status: 403, body: problem(403, 'forbidden') };
    });
    const refused: string[] = [];
    page.on('response', (r) => {
      if (r.status() === 403) refused.push(new URL(r.url()).pathname);
    });

    await page.goto('/');
    const side = page.getByRole('navigation', { name: 'Main' });
    await expect(side.getByRole('link')).toHaveText(c.nav);
    // The first screen is a page, never the no-access state (a role without Pulse starts elsewhere).
    const main = page.getByRole('main');
    await expect(main.getByRole('heading', { level: 1 })).toBeVisible();
    await expect(main.getByText("You don't have access to this screen")).toHaveCount(0);

    for (const name of c.nav) {
      await side.getByRole('link', { name, exact: true }).click();
      await expect(side.getByRole('link', { name, exact: true })).toHaveAttribute('aria-current', 'page');
      await expect(main.getByRole('heading', { level: 1 })).toBeVisible();
      await expect(main.getByText("You don't have access to this screen")).toHaveCount(0);
    }
    await page.waitForLoadState('networkidle');
    expect(refused, 'admin calls refused in a normal flow').toEqual([]);

    if (c.hidden) {
      await page.goto(c.hidden);
      await expect(main.getByRole('heading', { name: "You don't have access to this screen" })).toBeVisible();
      await expect(side.getByRole('link')).toHaveText(c.nav);
    }
    // The only console errors allowed are the failed loads of 403s, and there are none.
    expect(guard.errors, 'console and page errors').toEqual([]);
  });
}
