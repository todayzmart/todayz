import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page } from '@playwright/test';
import { NOW, answerWith, problem, signIn, type Answer } from './fake-api';

/**
 * One journey per W3-05 area against the built console, with the API faked statefully: what the
 * page sends is checked, and the answer is the next state. The unit tests cover the edge cases;
 * this proves the built bundle wires the same flows, and leaves a screenshot of each.
 */

const BUSINESS_ID = '01JBUS1NESS0000000000000B1';
const STORE_ID = '01JST0RE000000000000000S01';
const REVIEW_ID = '01JREV1EW00000000000000V01';

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  await page.setViewportSize(VIEWPORTS.desktop);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

const page1 = (items: unknown[]) => ({ status: 200, body: { items, nextCursor: null, serverNow: NOW } });

/** Records each write's path and body, so a test can check what the page sent. */
async function fake(page: Page, path: string, reply: (method: string, url: URL, body: unknown) => Answer) {
  const sent: { path: string; body: unknown }[] = [];
  await answerWith(page, path, (req, url) => {
    const raw = req.postData();
    const body = raw ? (JSON.parse(raw) as unknown) : undefined;
    if (req.method() !== 'GET') sent.push({ path: url.pathname, body });
    return reply(req.method(), url, body);
  });
  return sent;
}

test('A-03: an admin verifies a submitted business from the queue', async ({ page }) => {
  await signIn(page, ['admin']);
  let status = 'submitted';
  const detail = () => ({
    id: BUSINESS_ID,
    brandName: 'Sri Lakshmi Stores',
    legalName: 'Sri Lakshmi Traders',
    status,
    needsInfoFields: [],
    needsInfoNote: null,
    submittedAt: '2026-10-24T05:00:00Z',
    verifiedAt: status === 'verified' ? NOW : null,
    createdAt: '2026-10-20T05:00:00Z',
    kyc: { status: 'submitted', legalName: 'Sri Lakshmi Traders', panLast4: '234F', gstinLast4: '1Z5Q', upiLast4: null, bankLast4: '6789', bankIfsc: 'HDFC0001234', notes: null, verifiedAt: null },
    stores: [{ id: STORE_ID, name: 'Lakshmi, Jayanagar', status: 'pending_review', locality: 'Jayanagar', city: 'Bengaluru', liveAt: null, createdAt: '2026-10-21T05:00:00Z' }],
    staff: [{ id: '01JSTAFF000000000000000F01', name: 'Ravi', role: 'owner', status: 'active', phoneMasked: '98•••••321' }],
    documents: [],
    serverNow: NOW,
  });
  const sent = await fake(page, '/v1/admin/businesses**', (method, url) => {
    if (url.pathname === '/v1/admin/businesses') {
      const d = detail();
      return page1([{ id: d.id, brandName: d.brandName, legalName: d.legalName, status, submittedAt: d.submittedAt, createdAt: d.createdAt, city: 'Bengaluru', storeCount: 1 }]);
    }
    if (url.pathname.endsWith('/verify')) {
      status = 'verified';
      return { status: 200, body: detail() };
    }
    return method === 'GET' ? { status: 200, body: detail() } : undefined;
  });

  await page.goto('/businesses');
  const table = page.getByRole('table', { name: 'Businesses: Submitted' });
  await shot(page, 'admin-a03-queue.png');
  await table.getByRole('link', { name: 'Sri Lakshmi Stores' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Sri Lakshmi Stores' })).toBeVisible();
  await expect(page.getByText('•••• 234F')).toBeVisible();
  await page.getByRole('button', { name: 'Verify', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Verify Sri Lakshmi Stores?' });
  await dialog.getByRole('button', { name: 'Verify', exact: true }).click();
  await expect(page.getByText('Business verified')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Verify', exact: true })).toHaveCount(0);
  await expect(page.getByRole('complementary', { name: 'History' })).toBeVisible();
  expect(sent).toEqual([{ path: `/v1/admin/businesses/${BUSINESS_ID}/verify`, body: {} }]);
  await shot(page, 'admin-a03-verified.png', { fullPage: true });
});

test('A-05: an admin closes a live store until a time in IST', async ({ page }) => {
  await signIn(page, ['admin']);
  let closedUntil: string | null = null;
  const detail = () => ({
    id: STORE_ID,
    name: 'Lakshmi, Jayanagar',
    businessId: BUSINESS_ID,
    businessName: 'Sri Lakshmi Stores',
    businessStatus: 'verified',
    status: 'live',
    addressLine1: '12, 4th Block',
    addressLine2: null,
    landmark: null,
    locality: 'Jayanagar',
    city: 'Bengaluru',
    pincode: '560011',
    location: { lat: 12.925, lng: 77.593 },
    hours: { weekly: { mon: [{ open: '09:00', close: '21:00' }] }, holidays: [] },
    closedUntil,
    reviewNote: null,
    liveAt: '2026-10-21T05:00:00Z',
    reviewedAt: '2026-10-21T05:00:00Z',
    ratingAvg: 4.2,
    ratingCount: 12,
    acceptRate30d: 0.94,
    unacceptedExpiryRate30d: 0.03,
    reliabilityScore: 0.91,
    reliabilityComputedAt: '2026-10-25T00:00:00Z',
    activeStrikes: 0,
    strikes: [],
    createdAt: '2026-10-20T05:00:00Z',
    serverNow: NOW,
  });
  const sent = await fake(page, '/v1/admin/stores**', (method, url, body) => {
    if (url.pathname === '/v1/admin/stores') {
      const d = detail();
      const { id, name, businessId, businessName, businessStatus, status, locality, city, activeStrikes, reliabilityScore, createdAt } = d;
      return page1([{ id, name, businessId, businessName, businessStatus, status, locality, city, closedUntil, activeStrikes, reliabilityScore, createdAt }]);
    }
    if (url.pathname.endsWith('/force-close')) {
      closedUntil = (body as { until: string }).until;
      return { status: 200, body: detail() };
    }
    return method === 'GET' ? { status: 200, body: detail() } : undefined;
  });

  await page.goto('/stores');
  await shot(page, 'admin-a05-list.png');
  await page.getByRole('table', { name: 'Stores' }).getByRole('link', { name: 'Lakshmi, Jayanagar' }).click();
  await page.getByRole('button', { name: 'Close for a while' }).click();
  const dialog = page.getByRole('dialog', { name: 'Close Lakshmi, Jayanagar for a while?' });
  await dialog.getByLabel('Reason').fill('Owner unwell');
  await dialog.getByLabel('Closed until (IST)').fill('2026-10-26T09:00');
  await shot(page, 'admin-a05-force-close.png');
  await dialog.getByRole('button', { name: 'Close for a while' }).click();
  await expect(page.getByText('Store closed')).toBeVisible();
  // 09:00 IST is 03:30 UTC.
  expect(sent).toEqual([{ path: `/v1/admin/stores/${STORE_ID}/force-close`, body: { reason: 'Owner unwell', until: '2026-10-26T03:30:00.000Z' } }]);
});

test('A-12: support hides a reported review and restores it, each with a reason', async ({ page }) => {
  await signIn(page, ['support']);
  let hidden = false;
  let hideReason: string | null = null;
  const review = () => ({
    id: REVIEW_ID,
    storeId: STORE_ID,
    storeName: 'Sri Lakshmi Stores',
    customerId: '01JCUST0000000000000000C01',
    orderId: '01JORDER000000000000000R01',
    posSaleId: null,
    stars: 1,
    text: 'Rude at the counter and the rice was stale.',
    tags: ['service'],
    createdAt: '2026-10-24T10:00:00Z',
    hidden,
    hiddenAt: hidden ? NOW : null,
    hideReason,
    openReports: hidden ? 0 : 1,
    reply: null,
  });
  const sent = await fake(page, '/v1/admin/reviews**', (method, url, body) => {
    if (url.pathname === '/v1/admin/reviews') return page1([review()]);
    if (url.pathname.endsWith('/hide')) {
      if (hidden) return { status: 409, body: problem(409, 'invalid_transition') };
      hidden = true;
      hideReason = (body as { reason: string }).reason;
      return { status: 200, body: review() };
    }
    if (url.pathname.endsWith('/restore')) {
      hidden = false;
      hideReason = null;
      return { status: 200, body: review() };
    }
    return method === 'GET' ? { status: 200, body: review() } : undefined;
  });

  await page.goto('/reviews');
  await page.getByRole('button', { name: /Rude at the counter/ }).click();
  await expect(page.getByRole('complementary', { name: 'History' }).getByText('No changes yet.')).toBeVisible();
  await shot(page, 'admin-a12-detail.png');
  await page.getByRole('button', { name: 'Hide review' }).click();
  let dialog = page.getByRole('dialog', { name: 'Hide this review of Sri Lakshmi Stores?' });
  await dialog.getByLabel('Reason').fill('Abusive language');
  await dialog.getByRole('button', { name: 'Hide review' }).click();
  await page.getByRole('button', { name: 'Restore review' }).click();
  dialog = page.getByRole('dialog', { name: 'Restore this review of Sri Lakshmi Stores?' });
  await dialog.getByLabel('Reason').fill('Owner and customer settled it');
  await dialog.getByRole('button', { name: 'Restore review' }).click();
  await expect(page.getByRole('button', { name: 'Hide review' })).toBeVisible();
  expect(sent).toEqual([
    { path: `/v1/admin/reviews/${REVIEW_ID}/hide`, body: { reason: 'Abusive language' } },
    { path: `/v1/admin/reviews/${REVIEW_ID}/restore`, body: { reason: 'Owner and customer settled it' } },
  ]);
});

test('A-08: the catalogue desk merges a store-made duplicate into the master product', async ({ page }) => {
  await signIn(page, ['catalogue']);
  const TOOR = '01JPR0DXCT00000000000000T1';
  const PENDING = '01JPR0DXCT00000000000000P1';
  const CATEGORY = '01JCATEG0RY0000000000000S1';
  const product = {
    id: TOOR,
    name: 'Toor Dal',
    brandName: 'Tata Sampann',
    categoryId: CATEGORY,
    categoryName: 'Staples',
    variant: 'Unpolished',
    packSize: 1,
    unit: 'kg',
    mrpPaise: 18900,
    barcodes: ['8901725181234'],
    status: 'active',
    mergedInto: null,
    storeCount: 14,
    createdAt: '2026-10-01T09:00:00Z',
    updatedAt: '2026-10-20T09:00:00Z',
  };
  const pending = { ...product, id: PENDING, name: 'Toor dal unpolished', brandName: null, variant: null, mrpPaise: null, barcodes: [], status: 'pending_review', storeCount: 0 };
  let waiting = true;
  await answerWith(page, '/v1/admin/categories', () => ({
    status: 200,
    body: { items: [{ id: CATEGORY, parentId: null, slug: 'staples', names: { en: 'Staples' }, tint: 'amber', icon: 'wheat', sort: 10, productCount: 240, updatedAt: NOW }] },
  }));
  const sent = await fake(page, '/v1/admin/pending-products**', (method, url) => {
    if (method === 'GET') {
      const items = waiting ? [{ product: pending, createdByStoreId: STORE_ID, createdByStoreName: 'Annapurna Kirana', suggestions: [{ product, similarity: 0.62 }] }] : [];
      return { status: 200, body: { items, assistEnabled: false, nextCursor: null, serverNow: NOW } };
    }
    if (url.pathname.endsWith('/merge')) {
      waiting = false;
      return { status: 200, body: { ...pending, status: 'merged', mergedInto: TOOR } };
    }
    return undefined;
  });

  await page.goto('/catalogue?tab=review');
  await page.getByRole('table', { name: 'Products to review' }).getByRole('button', { name: 'Toor dal unpolished' }).click();
  await expect(page.getByText('1 kg · 62% alike · 14 stores')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Add product' })).toHaveCount(0);
  await shot(page, 'admin-a08-review.png');
  await page.getByRole('button', { name: 'Merge into this' }).click();
  await page.getByRole('dialog', { name: 'Merge Toor dal unpolished into Toor Dal · Unpolished?' }).getByRole('button', { name: 'Merge' }).click();
  await expect(page.getByText('This product is no longer waiting for review. Its history is below.')).toBeVisible();
  expect(sent).toEqual([{ path: `/v1/admin/pending-products/${PENDING}/merge`, body: { intoProductId: TOOR } }]);
});

test('A-20: support clears a held banner, then takes it down with a reason', async ({ page }) => {
  await signIn(page, ['support']);
  const BANNER = '01JBANNER00000000000000B01';
  let status = 'pending_review';
  let moderation = 'needs_review';
  let removeReason: string | null = null;
  const banner = () => ({
    id: BANNER,
    storeId: STORE_ID,
    storeName: 'Sri Lakshmi Stores',
    storeCity: 'Bengaluru',
    headline: 'Diwali sweets 20% off',
    imageKey: null,
    template: { tint: 'amber', subline: 'Kaju katli, laddoo and more' },
    targetType: 'offer',
    targetLabel: 'Diwali sweets',
    targetItemCount: 0,
    startsAt: '2026-10-24T00:00:00Z',
    endsAt: '2026-11-02T00:00:00Z',
    status,
    moderation,
    removeReason,
    removedAt: removeReason ? NOW : null,
    createdAt: '2026-10-24T10:00:00Z',
    updatedAt: NOW,
  });
  const tab = () => (status === 'pending_review' ? 'review' : status === 'removed' ? 'removed' : 'live');
  const sent = await fake(page, '/v1/admin/banners**', (method, url, body) => {
    if (url.pathname === '/v1/admin/banners') return page1(tab() === (url.searchParams.get('state') ?? 'review') ? [banner()] : []);
    if (url.pathname.endsWith('/approve')) {
      status = 'live';
      moderation = 'auto_ok';
      return { status: 200, body: banner() };
    }
    if (url.pathname.endsWith('/takedown')) {
      if (status === 'removed') return { status: 409, body: problem(409, 'invalid_transition') };
      status = 'removed';
      removeReason = (body as { reason: string }).reason;
      return { status: 200, body: banner() };
    }
    return method === 'GET' ? { status: 200, body: banner() } : undefined;
  });

  await page.goto('/banners');
  await page.getByRole('table', { name: 'Waiting for review' }).getByRole('button', { name: 'Diwali sweets 20% off' }).click();
  await expect(page.getByRole('complementary', { name: 'History' }).getByText('No changes yet.')).toBeVisible();
  await shot(page, 'admin-a20-banners.png');
  await page.getByRole('button', { name: 'Clear banner' }).click();
  await page.getByRole('dialog', { name: 'Clear the banner from Sri Lakshmi Stores?' }).getByRole('button', { name: 'Clear banner' }).click();
  await page.getByRole('tab', { name: 'Live and scheduled' }).click();
  await page.getByRole('table', { name: 'Live and scheduled' }).getByRole('button', { name: 'Diwali sweets 20% off' }).click();
  await page.getByRole('button', { name: 'Take down' }).click();
  const dialog = page.getByRole('dialog', { name: 'Take down the banner from Sri Lakshmi Stores?' });
  await dialog.getByLabel('Reason').fill('Price on the banner is misleading');
  await dialog.getByRole('button', { name: 'Take down' }).click();
  await expect(page.getByText('That banner is not in this list. It may have moved to another tab.')).toBeVisible();
  expect(sent).toEqual([
    { path: `/v1/admin/banners/${BANNER}/approve`, body: {} },
    { path: `/v1/admin/banners/${BANNER}/takedown`, body: { reason: 'Price on the banner is misleading' } },
  ]);
});

test('A-17: on a phone each setting reads as a card, with its value, range and change button in view', async ({ page }) => {
  await signIn(page, ['admin']);
  const setting = (key: string, group: string, unit: string, value: number, min: number, max: number, description: string, money = false) => ({
    key,
    group,
    unit,
    value,
    min,
    max,
    description,
    money,
    editable: !money,
    updatedBy: null,
    updatedByName: null,
    updatedAt: '2026-10-20T09:30:00Z',
    version: 1,
  });
  await answerWith(page, '/v1/admin/settings', () => ({
    status: 200,
    body: {
      items: [
        setting('hold_minutes', 'stock', 'minutes', 30, 5, 240, 'How long a booked item is held for pickup. 01 §4'),
        setting('ask_max_stores', 'asks', 'count', 5, 1, 20, 'Stores one ask goes to. 01 §15'),
        setting('delivery_fee_paise', 'orders', 'paise', 2500, 0, 50_000, 'Delivery fee. 01 §5', true),
      ],
      providers: [{ name: 'SMS_API_KEY', set: true }],
      serverNow: NOW,
    },
  }));
  await page.setViewportSize(VIEWPORTS.phone);
  await page.goto('/settings');
  const row = page.getByRole('row', { name: /hold_minutes/ });
  await expect(row.getByText('30 minutes')).toBeInViewport();
  await expect(row.getByRole('button', { name: 'Change hold_minutes' })).toBeInViewport();
  await shot(page, 'admin-a17-phone.png', { fullPage: true });
});

test('A-18: a super admin invites a desk member, then disables another after a fresh code', async ({ page }) => {
  await signIn(page, ['super_admin']);
  const MEERA = '01JADM1N0000000000000000M1';
  const RAVI = '01JADM1N0000000000000000N2';
  const account = (id: string, name: string, roles: string[], status: string, version: number) => ({
    id,
    name,
    phoneMasked: id === MEERA ? '90•••••311' : '90•••••412',
    roles,
    status,
    createdBy: '01JADMIN0000000000000000AD',
    createdByName: 'Asha Rao',
    createdAt: '2026-10-20T09:30:00Z',
    disabledAt: status === 'disabled' ? NOW : null,
    lastActiveAt: id === MEERA ? '2026-10-25T05:12:00Z' : null,
    signInLockedUntil: null,
    version,
  });
  let meera = account(MEERA, 'Meera', ['support'], 'active', 1);
  let ravi: ReturnType<typeof account> | null = null;
  let freshCode = false;
  const sent = await fake(page, '/v1/admin/users**', (method, url, body) => {
    if (url.pathname === '/v1/admin/users' && method === 'GET') {
      const want = url.searchParams.get('status') ?? 'active';
      return page1([meera, ...(ravi ? [ravi] : [])].filter((a) => a.status === want));
    }
    if (url.pathname === '/v1/admin/users' && method === 'POST') {
      const b = body as { name: string; roles: string[] };
      ravi = account(RAVI, b.name, b.roles, 'active', 1);
      return { status: 201, body: ravi };
    }
    if (url.pathname === `/v1/admin/users/${MEERA}` && method === 'PATCH') {
      if (!freshCode) return { status: 403, body: problem(403, 'step_up_required') };
      meera = account(MEERA, 'Meera', ['support'], 'disabled', 2);
      return { status: 200, body: meera };
    }
    return undefined;
  });
  // The SMS step-up (D-264): send answers a request id, verify stamps this sign-in; the token stays.
  await answerWith(page, '/v1/auth/step-up/send', () => ({ status: 200, body: { requestId: '01JC0000000000000000000SMS', resendAfter: 30 } }));
  await answerWith(page, '/v1/auth/step-up/verify', () => {
    freshCode = true;
    return { status: 200, body: { validUntil: new Date(Date.now() + 10 * 60_000).toISOString() } };
  });

  await page.goto('/admin-users');
  await page.getByRole('table', { name: 'Active' }).getByRole('button', { name: 'Meera' }).click();
  await expect(page.getByRole('complementary', { name: 'History' }).getByText('No changes yet.')).toBeVisible();
  await shot(page, 'admin-a18-admin-users.png');

  await page.getByRole('button', { name: 'Invite admin' }).click();
  const invite = page.getByRole('dialog', { name: 'Invite an admin' });
  await invite.getByLabel('Mobile number').fill('90000 00412');
  await invite.getByLabel('Name').fill('Ravi');
  await invite.getByLabel('Catalogue').check();
  await invite.getByLabel('Reason').fill('Joins the catalogue desk');
  await invite.getByRole('button', { name: 'Invite admin' }).click();
  await expect(page.getByText('Ravi can now sign in to the console.')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Ravi', level: 2 })).toBeVisible();

  await page.getByRole('table', { name: 'Active' }).getByRole('button', { name: 'Meera' }).click();
  await page.getByRole('button', { name: 'Disable' }).click();
  const disable = page.getByRole('dialog', { name: 'Disable Meera?' });
  await disable.getByLabel('Reason').fill('Left the team');
  await disable.getByRole('button', { name: 'Disable' }).click();
  const step = page.getByRole('dialog', { name: 'Confirm it is you' });
  await step.getByRole('button', { name: 'Send code' }).click();
  const boxes = step.getByRole('group', { name: 'Code from the SMS' }).locator('input');
  for (const [i, d] of '123456'.split('').entries()) await boxes.nth(i).fill(d);
  await expect(page.getByText('Meera is disabled and signed out.')).toBeVisible();
  expect(sent).toEqual([
    { path: '/v1/admin/users', body: { phone: '90000 00412', name: 'Ravi', roles: ['catalogue'], reason: 'Joins the catalogue desk' } },
    { path: `/v1/admin/users/${MEERA}`, body: { status: 'disabled', version: 1, reason: 'Left the team' } },
    { path: `/v1/admin/users/${MEERA}`, body: { status: 'disabled', version: 1, reason: 'Left the team' } },
  ]);
  // The browser logs the faked 403 step_up_required; that one is the point of the journey.
  const stepUp = guard.errors.findIndex((e) => e.includes('status of 403'));
  expect(stepUp).toBeGreaterThanOrEqual(0);
  guard.errors.splice(stepUp, 1);
});

test('A-02: an analyst reads Pulse, switches to 30 days and sees a store needing attention without a link', async ({ page }) => {
  await signIn(page, ['analyst']);
  const day = (date: string, i: number) => ({
    date,
    gmvPaise: (800 + i * 90) * 100,
    posGmvPaise: (300 + i * 20) * 100,
    orders: 8 + i,
    completedOrders: 7 + i,
    activeCustomers: 40 + i * 3,
    newCustomers: 4,
    liveStores: 12,
    searches: 180 + i * 10,
    zeroResultSearches: 18,
  });
  const ranges: string[] = [];
  await answerWith(page, '/v1/admin/pulse?*', (_req, url) => {
    const range = url.searchParams.get('range') ?? '7d';
    ranges.push(range);
    const days = range === '30d' ? 30 : 7;
    return {
      status: 200,
      body: {
        range,
        from: range === '30d' ? '2026-09-26' : '2026-10-19',
        to: '2026-10-25',
        asOf: '2026-10-25T06:25:00Z',
        kpis: {
          gmvPaise: { value: 7_240_00, previous: 6_000_00 },
          onlineGmvPaise: { value: 4_440_00, previous: 4_000_00 },
          posGmvPaise: { value: 2_800_00, previous: 2_000_00 },
          orders: { value: 70, previous: 80 },
          completedOrders: { value: 58, previous: 60 },
          successRateBp: { value: 8286, previous: 7500 },
          activeCustomers: { value: 340, previous: 300 },
          transactingCustomers: { value: 61, previous: 55 },
          liveStores: { value: 12, previous: 11 },
          activeStores: { value: 15, previous: 14 },
          newStores: { value: 2, previous: 1 },
          searches: { value: 1400, previous: 1200 },
          searchSuccessRateBp: { value: 9000, previous: 8800 },
          d7RetentionBp: { value: 2200, previous: 2500 },
        },
        trend: Array.from({ length: days }, (_, i) => {
          const d = new Date(Date.UTC(2026, 9, 25 - (days - 1 - i)));
          return day(d.toISOString().slice(0, 10), i % 9);
        }),
        funnel: { searches: 1400, searchesWithResults: 1260, storeViews: 600, ordersPlaced: 70, ordersAccepted: 63, ordersCompleted: 58 },
        leaks: [
          { status: 'expired', reason: 'not_accepted', count: 6 },
          { status: 'rejected', reason: 'out_of_stock', count: 4 },
          { status: 'cancelled', reason: 'changed_mind', count: 2 },
        ],
        retention: {
          d1Bp: 4100,
          d7Bp: 2200,
          d30Bp: 1200,
          cohorts: [
            { cohortWeek: '2026-10-19', size: 30, weeksBp: [10000] },
            { cohortWeek: '2026-10-12', size: 42, weeksBp: [10000, 3300] },
            { cohortWeek: '2026-10-05', size: 38, weeksBp: [10000, 2900, 2100] },
          ],
        },
        credits: { issued: 500, spent: 320, expired: 10 },
        zeroResultSearches: [
          { queryNorm: 'paneer', searches: 14 },
          { queryNorm: 'ragi flour', searches: 11 },
        ],
        storesNeedingAttention: [
          { storeId: STORE_ID, name: 'Lakshmi Stores', city: 'Bengaluru', reason: 'not_accepting', expiredUnaccepted: 4, strikes30d: 1, acceptRateBp: 6000, stockUpdatedAt: '2026-10-24T05:00:00Z' },
        ],
        serverNow: NOW,
      },
    };
  });
  await answerWith(page, '/v1/admin/pulse/map*', () => ({
    status: 200,
    body: {
      range: '7d',
      from: '2026-10-19',
      to: '2026-10-25',
      stores: [
        { storeId: STORE_ID, name: 'Lakshmi Stores', city: 'Bengaluru', status: 'live', lat: 12.97, lng: 77.59, live: true },
        { storeId: '01JST0RE000000000000000S02', name: 'Sri Ganesh', city: 'Mysuru', status: 'live', lat: 12.3, lng: 76.64, live: true },
      ],
      demand: [{ cell: '8860145b49fffff', searches: 40, zeroResultSearches: 6 }],
      serverNow: NOW,
    },
  }));

  await page.goto('/');
  const main = page.getByRole('main');
  await expect(main.getByText('₹7,240')).toBeVisible();
  await expect(main.getByRole('img', { name: /GMV per day from 19 Oct to 25 Oct/ })).toBeVisible();
  const attention = main.getByRole('table', { name: 'Stores needing attention' });
  await expect(attention.getByText('Lakshmi Stores')).toBeVisible();
  await expect(attention.getByRole('link')).toHaveCount(0);
  await shot(page, 'admin-a02-pulse.png', { fullPage: true });

  await main.getByRole('radio', { name: '30 days' }).click();
  await expect(page).toHaveURL(/\?range=30d$/);
  await expect(main.getByRole('img', { name: /from 26 Sept? to 25 Oct/ })).toBeVisible();
  expect(ranges).toContain('30d');
});

test('A-16: an admin reads the channels and a kind in Hindi, then sends a test to their own inbox', async ({ page }) => {
  await signIn(page, ['admin']);
  const text = (lang: string, variant: string | null, body: string, fallback = false) => ({ lang, variant, title: variant ? 'Order ready' : 'Order update', body, fallback });
  const orderUpdate = {
    kind: 'order_update',
    app: 'customer',
    channels: ['push'],
    params: ['store', 'variant'],
    texts: [
      text('en', null, 'Your order from {store} has an update.'),
      text('en', 'ready', 'Your order at {store} is ready to collect.'),
      text('hi', null, '{store} से आपके ऑर्डर में बदलाव है।'),
      text('hi', 'ready', '{store} पर आपका ऑर्डर तैयार है।'),
      ...['kn', 'te', 'ta'].flatMap((l) => [text(l, null, 'Your order from {store} has an update.', true), text(l, 'ready', 'Your order at {store} is ready to collect.', true)]),
    ],
    providerTemplates: [],
    testable: true,
  };
  const orderUnseen = {
    kind: 'order_unseen',
    app: 'business',
    channels: ['sms', 'whatsapp'],
    params: ['code'],
    texts: [text('en', null, 'Order {code} is waiting for you.')],
    providerTemplates: [{ lang: 'en', name: 'tz_order_unseen_en', smsTemplateIdSet: false }],
    testable: false,
  };
  await fake(page, '/v1/admin/integrations', () => ({
    status: 200,
    body: {
      items: [
        { channel: 'push', enabled: false, senderBuilt: false, vars: [{ name: 'VAPID_PRIVATE_KEY', set: false }], lastError: null },
        {
          channel: 'sms',
          enabled: true,
          senderBuilt: true,
          vars: [
            { name: 'SMS_API_URL', set: true },
            { name: 'SMS_SENDER_ID', set: true },
            { name: 'SMS_DLT_ENTITY_ID', set: false },
          ],
          lastError: { at: '2026-10-25T05:50:00Z', error: 'throttled' },
        },
        { channel: 'whatsapp', enabled: false, senderBuilt: false, vars: [{ name: 'WHATSAPP_API_KEY', set: false }], lastError: null },
      ],
      serverNow: NOW,
    },
  }));
  const sent = await fake(page, '/v1/admin/notification-templates**', (method, url, body) => {
    if (url.pathname === '/v1/admin/notification-templates' && method === 'GET') {
      const app = url.searchParams.get('app');
      return { status: 200, body: { items: [orderUnseen, orderUpdate].filter((t) => !app || t.app === app), serverNow: NOW } };
    }
    if (url.pathname === '/v1/admin/notification-templates/test-send') {
      const b = body as { kind: string; variant?: string | null };
      return { status: 201, body: { notificationId: '01JN0T1F000000000000000001', kind: b.kind, variant: b.variant ?? null, sentAt: NOW } };
    }
    return undefined;
  });

  await page.goto('/notifications');
  const sms = page.getByRole('listitem', { name: 'SMS' });
  await expect(sms.getByText('SMS_DLT_ENTITY_ID')).toBeVisible();
  await expect(sms.getByText(/^Last failure .*: throttled$/)).toBeVisible();

  await page.getByRole('radio', { name: 'Customer' }).click();
  await page.getByRole('table', { name: 'Notification kinds' }).getByRole('button', { name: 'Order update' }).click();
  const card = page.getByRole('region', { name: 'Order update' });
  await card.getByRole('tab', { name: 'Hindi' }).click();
  await expect(page).toHaveURL(/[?&]lang=hi/);
  await expect(card.getByRole('article', { name: 'ready' })).toContainText('पर आपका ऑर्डर तैयार है।');
  await shot(page, 'admin-a16-notifications.png', { fullPage: true });

  await card.getByRole('article', { name: 'ready' }).getByRole('button', { name: 'Send a test to me' }).click();
  await expect(page.getByText('Test sent to your in-app inbox')).toBeVisible();
  expect(sent).toEqual([{ path: '/v1/admin/notification-templates/test-send', body: { kind: 'order_update', variant: 'ready' } }]);
});
