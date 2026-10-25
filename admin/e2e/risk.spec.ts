import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page } from '@playwright/test';
import { NOW, answerWith, problem, signIn, type Answer } from './fake-api';

/**
 * W3-02 journeys against the built console, with the API faked statefully (as in areas.spec.ts):
 * A-13 confirms a flag and blocks the customer as its own step; A-10 rejects an unpaid referral.
 */

const FLAG_ID = '01JF1AG0000000000000000F01';
const REFERRAL_ID = '01JREFERRA00000000000000R1';
const STORE_ID = '01JST0RE000000000000000S01';
const CUSTOMER_ID = '01JCUST0000000000000000C01';

let guard: PageGuard;

test.beforeEach(async ({ page }) => {
  guard = await guardPage(page);
  await page.setViewportSize(VIEWPORTS.desktop);
});

test.afterEach(() => {
  expect(guard.external, 'requests to non-local hosts').toEqual([]);
  expect(guard.errors, 'console and page errors').toEqual([]);
});

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

test('A-13: an admin reads a flag, confirms it, then blocks the customer as a separate step', async ({ page }) => {
  await signIn(page, ['admin']);
  let status: 'open' | 'confirmed' = 'open';
  let note: string | null = null;
  let blocked = false;
  const flag = () => ({
    id: FLAG_ID,
    rule: 'referral.self_like',
    subjectType: 'referral',
    subjectId: REFERRAL_ID,
    subjectLabel: 'Sri Lakshmi Stores → Asha',
    score: 70,
    status,
    createdAt: '2026-10-25T06:00:00Z',
    reviewedAt: note ? NOW : null,
    reviewedBy: note ? 'Ravi' : null,
    reviewNote: note,
  });
  const sent = await fake(page, '/v1/admin/risk/flags**', (method, url, body) => {
    if (url.pathname === '/v1/admin/risk/flags') {
      const want = url.searchParams.get('status') ?? 'open';
      return {
        status: 200,
        body: {
          items: status === want ? [flag()] : [],
          openByRule: status === 'open' ? [{ rule: 'referral.self_like', open: 1 }] : [],
          nextCursor: null,
          serverNow: NOW,
        },
      };
    }
    if (url.pathname.endsWith('/confirm')) {
      if (status !== 'open') return { status: 409, body: problem(409, 'invalid_transition') };
      status = 'confirmed';
      note = (body as { reason: string }).reason;
      return { status: 200, body: flag() };
    }
    if (method !== 'GET') return undefined;
    return {
      status: 200,
      body: {
        flag: flag(),
        subject: {
          type: 'referral',
          id: REFERRAL_ID,
          label: 'Sri Lakshmi Stores → Asha',
          status: 'qualified',
          storeId: STORE_ID,
          businessId: null,
          customerId: blocked ? null : CUSTOMER_ID,
        },
        facts: [
          { key: 'minutes_to_first_order', value: '4' },
          { key: 'same_device', value: 'true' },
          { key: 'store_id', value: STORE_ID },
        ],
        evidence: [
          { kind: 'referral', id: REFERRAL_ID, label: 'QR standee claim', storeName: 'Sri Lakshmi Stores', at: '2026-10-25T05:46:00Z', amountPaise: null, note: null },
          { kind: 'order', id: '01JORDER000000000000000R01', label: 'First order', storeName: 'Sri Lakshmi Stores', at: '2026-10-25T05:50:00Z', amountPaise: 12000, note: 'pickup' },
        ],
        history: [],
        assistEnabled: false,
        serverNow: NOW,
      },
    };
  });
  const blocks = await fake(page, `/v1/admin/customers/${CUSTOMER_ID}/block`, () => {
    blocked = true;
    return { status: 200, body: { id: CUSTOMER_ID } };
  });

  await page.goto('/risk');
  await page.getByRole('table', { name: 'Risk flags' }).getByRole('button', { name: 'Referral looks like self-referral' }).click();
  await expect(page.getByRole('table', { name: 'Evidence' }).getByText('First order')).toBeVisible();
  await expect(page.getByRole('complementary', { name: 'History' }).getByText('No changes yet.')).toBeVisible();
  await shot(page, 'admin-a13-detail.png');

  await page.getByRole('button', { name: 'Confirm' }).click();
  let dialog = page.getByRole('dialog', { name: /^Confirm / });
  await dialog.getByLabel('Reason').fill('Owner referred their own second phone');
  await dialog.getByRole('button', { name: 'Confirm' }).click();
  await expect(page.getByText('By Ravi: Owner referred their own second phone')).toBeVisible();
  expect(blocks).toEqual([]);

  await page.getByRole('button', { name: 'Block customer' }).click();
  dialog = page.getByRole('dialog', { name: 'Block this customer?' });
  await dialog.getByLabel('Reason').fill('Confirmed referral abuse');
  await dialog.getByRole('button', { name: 'Block customer' }).click();
  await expect(page.getByText('Customer blocked')).toBeVisible();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('button', { name: 'Block customer' })).toBeHidden();
  expect(sent).toEqual([{ path: `/v1/admin/risk/flags/${FLAG_ID}/confirm`, body: { reason: 'Owner referred their own second phone' } }]);
  expect(blocks).toEqual([{ path: `/v1/admin/customers/${CUSTOMER_ID}/block`, body: { reason: 'Confirmed referral abuse' } }]);
});

test('A-13: support reads the queue with no clear or confirm', async ({ page }) => {
  await signIn(page, ['support']);
  await answerWith(page, '/v1/admin/risk/flags**', (_req, url) =>
    url.pathname === '/v1/admin/risk/flags'
      ? {
          status: 200,
          body: {
            items: [
              {
                id: FLAG_ID,
                rule: 'store_strikes',
                subjectType: 'store',
                subjectId: STORE_ID,
                subjectLabel: 'Kaveri Fresh',
                score: 60,
                status: 'open',
                createdAt: '2026-10-25T04:00:00Z',
                reviewedAt: null,
                reviewedBy: null,
                reviewNote: null,
              },
            ],
            openByRule: [{ rule: 'store_strikes', open: 1 }],
            nextCursor: null,
            serverNow: NOW,
          },
        }
      : undefined,
  );
  await page.goto('/risk');
  await expect(page.getByRole('tab', { name: /Store strikes/ })).toBeVisible();
  await expect(page.getByRole('table', { name: 'Risk flags' }).getByText('Store · Kaveri Fresh')).toBeVisible();
  await shot(page, 'admin-a13-queue-support.png');
  await expect(page.getByRole('button', { name: 'Clear' })).toHaveCount(0);
});

test('A-10: an admin filters to flagged referrals and rejects an unpaid one with a reason', async ({ page }) => {
  await signIn(page, ['admin']);
  let status: 'qualified' | 'rejected' = 'qualified';
  const referral = () => ({
    id: REFERRAL_ID,
    kind: 'retailer_customer',
    status,
    source: 'qr_standee',
    code: 'K7QX3M',
    referrer: { type: 'store', id: STORE_ID, label: 'Sri Lakshmi Stores' },
    referee: { type: 'customer', id: CUSTOMER_ID, label: 'Asha' },
    referrerCredits: 50,
    refereeCredits: 30,
    rejectReason: status === 'rejected' ? 'admin_rejected' : null,
    qualifiedAt: status === 'qualified' ? '2026-10-25T05:50:00Z' : null,
    rewardedAt: null,
    createdAt: '2026-10-25T05:46:00Z',
    openFlags: 1,
    flagRules: ['referral.self_like'],
  });
  const lists: URLSearchParams[] = [];
  const sent = await fake(page, '/v1/admin/referrals**', (method, url) => {
    if (url.pathname === '/v1/admin/referrals') {
      lists.push(url.searchParams);
      return { status: 200, body: { items: [referral()], nextCursor: null, serverNow: NOW } };
    }
    if (method === 'POST' && url.pathname.endsWith('/reject')) {
      status = 'rejected';
      return { status: 200, body: referral() };
    }
    return undefined;
  });

  await page.goto('/referrals');
  await page.getByLabel('Only with open risk flags').check();
  await expect(page).toHaveURL(/flagged=true/);
  await expect.poll(() => lists.at(-1)?.get('flagged')).toBe('true');
  await page.getByRole('button', { name: 'K7QX3M' }).click();
  await expect(page.getByRole('heading', { name: 'Referral K7QX3M' })).toBeVisible();
  await expect(page.getByRole('complementary', { name: 'History' }).getByText('No changes yet.')).toBeVisible();
  await shot(page, 'admin-a10-detail.png');
  await page.getByRole('button', { name: 'Reject' }).click();
  const dialog = page.getByRole('dialog', { name: 'Reject referral K7QX3M?' });
  await dialog.getByLabel('Reason').fill('Owner referred their own second phone');
  await dialog.getByRole('button', { name: 'Reject' }).click();
  await expect(page.getByText('admin_rejected')).toBeVisible();
  expect(sent).toEqual([{ path: `/v1/admin/referrals/${REFERRAL_ID}/reject`, body: { reason: 'Owner referred their own second phone' } }]);
});
