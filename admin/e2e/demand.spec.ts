import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test } from '@playwright/test';
import { NOW, answerWith, signIn } from './fake-api';

/**
 * A-23 Demand (W3-16) against the built console with a stateful fake: the gaps list and its
 * city filter, then a site-strip approval with one product left out. Screenshots go to TZ_QA_DIR.
 */

const BLR = '01JC1TY0000000000000000B01';
const HYD = '01JC1TY0000000000000000H01';
const P1 = '01JPR0D0000000000000000P01';
const P2 = '01JPR0D0000000000000000P02';
const P3 = '01JPR0D0000000000000000P03';

const GAPS = [
  { cityId: BLR, cityName: 'Bengaluru', productId: P1, itemName: 'Amul taaza 1 L', category: 'Dairy', customers: 6, asks: 9, shopsAsked: 14, lastAskedAt: '2026-10-25T05:30:00Z' },
  { cityId: BLR, cityName: 'Bengaluru', productId: null, itemName: 'Banana leaf plates', category: null, customers: 4, asks: 4, shopsAsked: 7, lastAskedAt: '2026-10-24T12:10:00Z' },
  { cityId: HYD, cityName: 'Hyderabad', productId: P3, itemName: 'Haldiram bhujia 400 g', category: 'Snacks', customers: 3, asks: 5, shopsAsked: 6, lastAskedAt: '2026-10-23T09:00:00Z' },
];

const item = (productId: string, productName: string, category: string, searchers: number, rank: number) => ({
  productId,
  productName,
  category,
  searchers,
  searchesTotal: searchers * 2,
  rank,
  status: 'proposed' as const,
});

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  await page.setViewportSize(VIEWPORTS.desktop);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

test('A-23 gaps: every city, then one, never a customer', async ({ page }) => {
  await signIn(page, ['analyst']);
  await answerWith(page, '/v1/admin/demand-gaps**', () => ({ status: 200, body: { items: GAPS, days: 7, minCustomers: 3, serverNow: NOW } }));
  await page.goto('/demand');
  const table = page.getByRole('table', { name: 'Demand gaps' });
  await expect(table.getByText('Amul taaza 1 L')).toBeVisible();
  await expect(table.getByText('Not in the catalogue')).toBeVisible();
  await expect(page.getByText(/at least 3 customers in the last 7 days/)).toBeVisible();
  await expect(page.getByRole('tab', { name: 'Site strip' })).toHaveCount(0);
  await shot(page, 'admin-a23-gaps.png');

  await page.getByLabel('City', { exact: true }).selectOption(HYD);
  await expect(page).toHaveURL(new RegExp(`city=${HYD}`));
  await expect(table.getByText('Amul taaza 1 L')).toHaveCount(0);
  await expect(table.getByText('Haldiram bhujia 400 g')).toBeVisible();
  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Export CSV' }).click();
  expect((await download).suggestedFilename()).toBe('todayz-demand-gaps-hyderabad-2026-10-25.csv');
});

test('A-23 site strip: leave one out, approve, see what the site shows', async ({ page }) => {
  await signIn(page, ['catalogue']);
  const proposed = { builtOn: '2026-10-25', status: 'proposed', decidedAt: null, items: [item(P1, 'Amul taaza 1 L', 'Dairy', 18, 1), item(P2, 'Maggi 70 g', 'Snacks', 11, 2), item(P3, 'Haldiram bhujia 400 g', 'Snacks', 7, 3)] };
  let blr: Record<string, unknown> = { cityId: BLR, cityName: 'Bengaluru', citySlug: 'bengaluru', latest: proposed, approved: null };
  const hyd = { cityId: HYD, cityName: 'Hyderabad', citySlug: 'hyderabad', latest: null, approved: null };
  const sent: unknown[] = [];
  await answerWith(page, '/v1/admin/site-marquee**', (req, url) => {
    if (req.method() === 'GET' && url.pathname === '/v1/admin/site-marquee') return { status: 200, body: { cities: [blr, hyd], minSearchers: 5, serverNow: NOW } };
    if (req.method() === 'PUT' && url.pathname === `/v1/admin/site-marquee/${BLR}`) {
      const body = JSON.parse(req.postData() ?? '{}') as { productIds: string[] };
      sent.push(body);
      const items = proposed.items.map((i) => ({ ...i, status: body.productIds.includes(i.productId) ? 'approved' : 'rejected' }));
      const build = { ...proposed, status: 'approved', decidedAt: NOW, items };
      blr = { ...blr, latest: build, approved: build };
      return { status: 200, body: blr };
    }
    return undefined;
  });

  await page.goto('/demand?tab=strip');
  const card = page.getByRole('region', { name: 'Bengaluru' });
  await expect(card.getByText('Proposed on 2026-10-25')).toBeVisible();
  await expect(page.getByRole('region', { name: 'Hyderabad' }).getByText('Not approved yet: the site shows the curated list.')).toBeVisible();
  await card.getByRole('checkbox', { name: /Haldiram bhujia 400 g/ }).uncheck();
  await shot(page, 'admin-a23-strip-proposed.png', { fullPage: true });

  await card.getByRole('button', { name: 'Approve 2 products' }).click();
  await expect(page.getByText('Site strip for Bengaluru approved with 2 products')).toBeVisible();
  expect(sent).toEqual([{ builtOn: '2026-10-25', decision: 'approve', productIds: [P1, P2] }]);
  await expect(card.getByText(/On the site: built 2026-10-25/)).toBeVisible();
  await expect(card.getByText('No new proposal waiting.')).toBeVisible();
  await shot(page, 'admin-a23-strip-approved.png', { fullPage: true });
});
