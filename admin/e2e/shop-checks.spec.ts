import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page } from '@playwright/test';
import { NOW, answerWith, signIn } from './fake-api';

/**
 * A-24 Shop checks (D-274) against the built console, with the queue faked statefully: a field
 * agent marks one shop checked and rejects another with a reason, on desktop and at 360px with no
 * sideways scroll. The unit tests cover the edge cases; this proves the bundle wires the flow and
 * leaves the screenshots (`TZ_QA_DIR=docs/qa/seller-auto-onboarding`).
 */

const STORE = '01JST0RE000000000000000S01';
const shop = (id: string, brandName: string, over: Record<string, unknown> = {}) => ({
  id,
  brandName,
  legalName: `${brandName} Traders`,
  status: 'verified',
  checkStatus: 'unchecked',
  checkNote: null,
  checkedAt: null,
  category: 'Grocery',
  liveAt: '2026-10-25T05:30:00Z',
  createdAt: '2026-10-25T05:00:00Z',
  byFieldAgent: false,
  kycStatus: 'submitted',
  stores: [
    {
      id: STORE,
      name: `${brandName}, Basavanagudi`,
      status: 'live',
      addressLine1: '12 DVG Road',
      locality: 'Basavanagudi',
      city: 'Bengaluru',
      pincode: '560004',
      lat: 12.9416,
      lng: 77.5737,
      liveAt: '2026-10-25T05:30:00Z',
    },
  ],
  photos: [
    { id: `${id.slice(0, 24)}P1`, kind: 'storefront', storeId: STORE, status: 'accepted', previewUrl: `https://img.todayz.localhost/${id}/front.svg`, createdAt: NOW },
    { id: `${id.slice(0, 24)}P2`, kind: 'shop_photo', storeId: STORE, status: 'accepted', previewUrl: `https://img.todayz.localhost/${id}/inside.svg`, createdAt: NOW },
  ],
  ...over,
});

const PHOTO = `<svg xmlns="http://www.w3.org/2000/svg" width="320" height="200"><rect width="320" height="200" fill="honeydew"/><rect x="40" y="60" width="240" height="120" fill="mediumaquamarine"/></svg>`;

async function fakeQueue(page: Page) {
  let queue = [
    shop('01JSH0PCHECK000000000000B1', 'Ganesh Kirana'),
    shop('01JSH0PCHECK000000000000B2', 'Old Mart', { byFieldAgent: true, liveAt: '2026-10-24T09:10:00Z', kycStatus: 'missing' }),
    shop('01JSH0PCHECK000000000000B3', 'Fresh Basket', { category: 'Fruits and vegetables', liveAt: '2026-10-23T11:00:00Z', photos: [] }),
  ];
  await page.route('https://img.todayz.localhost/**', (route) => route.fulfill({ status: 200, contentType: 'image/svg+xml', body: PHOTO }));
  await answerWith(page, '/v1/admin/shop-checks*', () => ({ status: 200, body: { items: queue, nextCursor: null, serverNow: NOW } }));
  await answerWith(page, '/v1/admin/businesses/*/check', (_req, url) => {
    const id = url.pathname.split('/')[4];
    const s = queue.find((q) => q.id === id);
    queue = queue.filter((q) => q.id !== id);
    return { status: 200, body: { ...s, checkStatus: 'checked', checkedAt: NOW } };
  });
  await answerWith(page, '/v1/admin/businesses/*/check-reject', (_req, url) => {
    const id = url.pathname.split('/')[4];
    const s = queue.find((q) => q.id === id);
    queue = queue.filter((q) => q.id !== id);
    return { status: 200, body: { shop: { ...s, status: 'rejected', checkStatus: 'rejected' }, cancelledOrders: 2 } };
  });
}

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

test('A-24 desktop: a field agent works the queue, checks one shop and rejects another', async ({ page }) => {
  await signIn(page, ['field_agent']);
  await fakeQueue(page);
  await page.setViewportSize(VIEWPORTS.desktop);
  await page.goto('/shop-checks');
  await expect(page.getByRole('article')).toHaveCount(3);
  await shot(page, 'shop-checks-desktop.png');

  const first = page.getByRole('article').filter({ hasText: 'Ganesh Kirana' });
  await first.getByRole('button', { name: /Mark checked/ }).click();
  const dialog = page.getByRole('dialog');
  if (await dialog.isVisible().catch(() => false)) await dialog.getByRole('button', { name: /Mark checked/ }).click();
  await expect(page.getByRole('article')).toHaveCount(2);

  await page.getByRole('article').filter({ hasText: 'Old Mart' }).getByRole('button', { name: /Reject/ }).click();
  await page.getByRole('dialog').getByRole('textbox').fill('The shop was shut and the address is a house');
  await shot(page, 'shop-checks-reject-desktop.png');
  await page.getByRole('dialog').getByRole('button', { name: /Reject/ }).click();
  await expect(page.getByRole('article')).toHaveCount(1);
});

test('A-24 phone: the queue fits 360px with no sideways scroll', async ({ page }) => {
  await signIn(page, ['field_agent']);
  await fakeQueue(page);
  await page.setViewportSize({ width: 360, height: 780 });
  await page.goto('/shop-checks');
  await expect(page.getByRole('article')).toHaveCount(3);
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0);
  await shot(page, 'shop-checks-phone.png');
});
