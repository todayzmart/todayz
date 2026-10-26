import { VIEWPORTS, guardPage, shot, type PageGuard } from '@todayz/config/playwright';
import { expect, test, type Page } from '@playwright/test';
import { NOW, answerWith, signIn } from './fake-api';

/**
 * W3-13 against the built console: the admin assistants in A-08, A-14 and A-13, with the AI routes
 * faked (create 202, one `processing` read, then `ready`). Each proves the card is labelled a draft,
 * nothing is sent until the person confirms, and the confirm carries only their choices.
 */

const DRAFT = '01JDRAFT000000000000000D01';
const FLAG_ID = '01JF1AG0000000000000000F01';
const REFERRAL_ID = '01JREFERRA00000000000000R1';
const TID = '01JTICKET00000000000000T01';
const TOOR = '01JPR0DXCT00000000000000T1';
const PENDING = '01JPR0DXCT00000000000000P1';
const STAPLES = '01JCATEG0RY0000000000000S1';
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

/**
 * One feature's draft routes. The first read is `processing`, so the card shows its wait; confirm
 * settles the draft with the lines it was sent. Returns the bodies the page posted.
 */
async function fakeDraft(page: Page, feature: string, draft: Record<string, unknown>) {
  const sent: { path: string; body: unknown }[] = [];
  let reads = 0;
  let settled: Record<string, unknown> | null = null;
  await answerWith(page, `/v1/admin/ai/${feature}/drafts**`, (req, url) => {
    const raw = req.postData();
    const body = raw ? (JSON.parse(raw) as unknown) : undefined;
    if (req.method() === 'POST') sent.push({ path: url.pathname, body });
    if (url.pathname === `/v1/admin/ai/${feature}/drafts` && req.method() === 'POST') return { status: 202, body: { draftId: DRAFT } };
    if (url.pathname.endsWith('/confirm')) {
      const lines = (body as { lines: { lineId: string; outcome: string }[] }).lines;
      const status = lines.some((l) => l.outcome !== 'discarded') ? 'confirmed' : 'discarded';
      settled = { ...draft, status, outcomes: lines.map((l) => ({ lineId: l.lineId, outcome: l.outcome })) };
      return { status: 200, body: { draftId: DRAFT, status, lines: lines.map((l) => ({ lineId: l.lineId, outcome: l.outcome })) } };
    }
    reads += 1;
    if (settled) return { status: 200, body: settled };
    return { status: 200, body: reads === 1 ? { ...draft, status: 'processing' } : draft };
  });
  return sent;
}

const base = { draftId: DRAFT, promptVersion: 'v1', createdAt: NOW, outcomes: [], failure: null };

test('A-08: the clean-up draft merges a duplicate only after the reviewer chooses it', async ({ page }) => {
  await signIn(page, ['admin']);
  const product = {
    id: TOOR,
    name: 'Toor Dal',
    brandName: 'Tata Sampann',
    categoryId: STAPLES,
    categoryName: 'Staples',
    variant: 'Unpolished',
    packSize: 1,
    unit: 'kg',
    mrpPaise: 18900,
    barcodes: [],
    status: 'active',
    mergedInto: null,
    storeCount: 14,
    createdAt: NOW,
    updatedAt: NOW,
  };
  const pending = {
    product: { ...product, id: PENDING, name: 'toor dal unpolished 1kg', brandName: null, variant: null, mrpPaise: null, status: 'pending_review', storeCount: 0 },
    createdByStoreId: STORE_ID,
    createdByStoreName: 'Annapurna Kirana',
    suggestions: [{ product, similarity: 0.62 }],
  };
  await answerWith(page, '/v1/admin/pending-products**', (req) => (req.method() === 'GET' ? { status: 200, body: { items: [pending], assistEnabled: true, nextCursor: null, serverNow: NOW } } : undefined));
  const sent = await fakeDraft(page, 'catalogue-cleanup', {
    ...base,
    feature: 'catalogue_cleanup',
    status: 'ready',
    pendingId: PENDING,
    normalise: { lineId: 'l1', name: 'Toor Dal', brandName: null, variant: 'Unpolished', packSize: 1, unit: 'kg', categoryId: STAPLES, categoryName: 'Staples', canApply: true },
    duplicate: { lineId: 'l2', productId: TOOR, productName: 'Toor Dal', similarity: 0.62, reason: 'Same dal and the same 1 kg pack as an active product.', canApply: true },
    categoryNames: { lineId: 'l3', categoryId: STAPLES, names: { en: 'Staples', hi: 'राशन', kn: 'ದಿನಸಿ', te: 'సరుకులు', ta: 'மளிகை' }, canApply: true },
  });

  await page.goto(`/catalogue?tab=review&open=${PENDING}`);
  const card = page.getByTestId('ai-assist');
  await expect(card.getByText('AI draft')).toBeVisible();
  await card.getByRole('button', { name: 'Draft a clean-up' }).click();
  await expect(card.getByText('Same as Toor Dal (62% name match)')).toBeVisible();
  expect(sent).toEqual([{ path: '/v1/admin/ai/catalogue-cleanup/drafts', body: { pendingId: PENDING } }]);
  await card.getByRole('radiogroup', { name: 'What to do with “Clean fields”' }).getByRole('radio', { name: 'Use these fields' }).click();
  await card.getByRole('radiogroup', { name: 'What to do with “Likely duplicate”' }).getByRole('radio', { name: 'Merge into it' }).click();
  await expect(card.getByText('Merge or clean up, not both: skip one of the two lines.')).toBeVisible();
  await expect(card.getByRole('button', { name: 'Confirm choices' })).toBeDisabled();
  await page.evaluate(() => window.scrollTo(0, 0));
  await shot(page, 'admin-w313-a08-cleanup.png', { fullPage: true });

  await card.getByRole('radiogroup', { name: 'What to do with “Clean fields”' }).getByRole('radio', { name: 'Skip' }).click();
  await card.getByRole('button', { name: 'Confirm choices' }).click();
  await expect(card.getByText('Done. What you chose was applied and recorded in the audit log.')).toBeVisible();
  expect(sent.at(-1)?.body).toEqual({
    lines: [
      { lineId: 'l1', outcome: 'discarded' },
      { lineId: 'l2', outcome: 'accepted' },
      { lineId: 'l3', outcome: 'discarded' },
    ],
  });
});

test('A-14: triage sets the priority the agent picked and sends the reply they chose', async ({ page }) => {
  await signIn(page, ['support']);
  const ticket = {
    id: TID,
    number: 'SUP-001001',
    subject: 'Last week’s payout has not arrived',
    category: 'payouts',
    status: 'open',
    priority: 'normal',
    openerType: 'business',
    opener: { id: '01JOWNER00000000000000OW01', name: 'Lakshmi' },
    businessId: '01JB0BZ1A2B3C4D5E6F7G8H9J0',
    businessName: 'Lakshmi Traders',
    storeId: STORE_ID,
    storeName: 'Lakshmi Indiranagar',
    orderId: null,
    orderNumber: null,
    assignee: null,
    slaDueAt: new Date(Date.parse(NOW) + 45 * 60_000).toISOString(),
    overdue: false,
    lastMessageAt: NOW,
    createdAt: NOW,
  };
  await answerWith(page, `/v1/admin/tickets/${TID}`, (req) =>
    req.method() === 'GET'
      ? {
          status: 200,
          body: {
            ticket,
            order: null,
            firstResponseAt: null,
            resolvedAt: null,
            closedAt: null,
            closeReason: null,
            messages: [
              {
                id: '01JMSG0000000000000000M001',
                kind: 'reply',
                authorType: 'business',
                author: { id: ticket.opener.id, name: 'Lakshmi' },
                body: 'The payout for last week shows as sent but it is not in our bank account yet.',
                createdAt: NOW,
                attachments: [],
              },
            ],
            agents: [],
            assistEnabled: true,
            serverNow: NOW,
          },
        }
      : undefined,
  );
  const sent = await fakeDraft(page, 'ticket-triage', {
    ...base,
    feature: 'ticket_triage',
    status: 'ready',
    ticketId: TID,
    classify: { lineId: 'l1', category: 'payouts', priority: 'high', orderId: null, orderNumber: null, reason: 'A payout marked sent but not received for a week.' },
    summary: null,
    reply: { lineId: 'l3', text: 'Sorry for the wait, Lakshmi. We are checking the bank transfer for last week and will update you today.' },
  });

  await page.goto(`/support/${TID}`);
  const card = page.getByTestId('ai-assist');
  await card.getByRole('button', { name: 'Draft triage' }).click();
  await expect(card.getByText('A payout marked sent but not received for a week.')).toBeVisible();
  await card.getByRole('radiogroup', { name: 'What to do with “Category and priority”' }).getByRole('radio', { name: 'Edit' }).click();
  await card.getByRole('combobox', { name: 'Priority' }).selectOption('urgent');
  await card.getByRole('radiogroup', { name: 'What to do with “Reply draft”' }).getByRole('radio', { name: 'Send this reply' }).click();
  await page.evaluate(() => window.scrollTo(0, 0));
  await shot(page, 'admin-w313-a14-triage.png', { fullPage: true });

  await card.getByRole('button', { name: 'Confirm choices' }).click();
  await expect(card.getByText('Done. What you chose was applied and recorded in the audit log.')).toBeVisible();
  expect(sent.at(-1)?.body).toEqual({
    lines: [
      { lineId: 'l1', outcome: 'edited', value: { category: 'payouts', priority: 'urgent' } },
      { lineId: 'l3', outcome: 'accepted' },
    ],
  });
});

test('A-13: the explanation uses only the flag’s numbers and goes to the audit trail when chosen', async ({ page }) => {
  await signIn(page, ['admin']);
  const flag = {
    id: FLAG_ID,
    rule: 'referral.self_like',
    subjectType: 'referral',
    subjectId: REFERRAL_ID,
    subjectLabel: 'Sri Lakshmi Stores → Asha',
    score: 70,
    status: 'open',
    createdAt: '2026-10-25T06:00:00Z',
    reviewedAt: null,
    reviewedBy: null,
    reviewNote: null,
  };
  await answerWith(page, '/v1/admin/risk/flags**', (req, url) => {
    if (req.method() !== 'GET') return undefined;
    if (url.pathname === '/v1/admin/risk/flags') return { status: 200, body: { items: [flag], openByRule: [{ rule: flag.rule, open: 1 }], nextCursor: null, serverNow: NOW } };
    return {
      status: 200,
      body: {
        flag,
        subject: { type: 'referral', id: REFERRAL_ID, label: flag.subjectLabel, status: 'qualified', storeId: STORE_ID, businessId: null, customerId: CUSTOMER_ID, customerParty: 'referee', businessParty: null },
        facts: [
          { key: 'minutes_to_first_order', value: '4' },
          { key: 'same_device', value: 'true' },
        ],
        evidence: [{ kind: 'order', id: '01JORDER000000000000000R01', label: 'First order', storeName: 'Sri Lakshmi Stores', at: '2026-10-25T05:50:00Z', amountPaise: 12000, note: null }],
        history: [],
        assistEnabled: true,
        serverNow: NOW,
      },
    };
  });
  const sent = await fakeDraft(page, 'risk-summary', {
    ...base,
    feature: 'risk_summary',
    status: 'ready',
    flagId: FLAG_ID,
    lines: [{ lineId: 'l1', text: 'The referred customer placed a first order 4 minutes after the referral, from the same device as the shop.' }],
  });

  await page.goto(`/risk?flag=${FLAG_ID}`);
  const card = page.getByTestId('ai-assist');
  await card.getByRole('button', { name: 'Explain this flag' }).click();
  await expect(card.getByText(/placed a first order 4 minutes after/)).toBeVisible();
  await card.getByRole('radiogroup', { name: 'What to do with “Summary”' }).getByRole('radio', { name: 'Add to audit log' }).click();
  await page.evaluate(() => window.scrollTo(0, 0));
  await shot(page, 'admin-w313-a13-explain.png', { fullPage: true });

  await card.getByRole('button', { name: 'Confirm choices' }).click();
  await expect(card.getByText('Done. What you chose was applied and recorded in the audit log.')).toBeVisible();
  expect(sent.at(-1)?.body).toEqual({ lines: [{ lineId: 'l1', outcome: 'accepted' }] });
  await shot(page, 'admin-w313-a13-recorded.png');
});
