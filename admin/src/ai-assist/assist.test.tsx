import { ApiError } from '@todayz/api-client';
import type { AdminPendingProduct } from '@todayz/api-client/gen/model/adminPendingProduct';
import type { AdminProduct } from '@todayz/api-client/gen/model/adminProduct';
import type { CatalogueCleanupDraft } from '@todayz/api-client/gen/model/catalogueCleanupDraft';
import type { RiskFlag } from '@todayz/api-client/gen/model/riskFlag';
import type { RiskSummaryDraft } from '@todayz/api-client/gen/model/riskSummaryDraft';
import type { TicketDetail } from '@todayz/api-client/gen/model/ticketDetail';
import type { TicketTriageDraft } from '@todayz/api-client/gen/model/ticketTriageDraft';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { CUSTOMER_ID, NOW, STORE_ID, emptyAudit } from '../test/fixtures';
import { json, problem, renderAt, type Call, type Handler } from '../test/render';
import { assistErrorKey, decision, lineReasons } from './draft';

const DRAFT = '01JDRAFT000000000000000D01';
const KEY = /^[0-9A-HJKMNP-TV-Z]{26}$/;
const FLAG_ID = '01JF1AG0000000000000000F01';
const REFERRAL_ID = '01JREFERRA00000000000000R1';
const TID = '01JTICKET00000000000000T01';
const OWNER = '01JOWNER00000000000000OW01';
const TOOR = '01JPR0DXCT00000000000000T1';
const PENDING = '01JPR0DXCT00000000000000P1';
const STAPLES = '01JCATEG0RY0000000000000S1';

const base = { draftId: DRAFT, promptVersion: 'v1', createdAt: NOW, outcomes: [], failure: null };

/** The AI routes of one feature: create answers 202, the draft reads `ready`, confirm answers what `confirm` says. */
function draftApi<D>(path: string, draft: D, confirm: (c: Call) => Response = () => json(200, { draftId: DRAFT, status: 'confirmed', lines: [] })): Handler {
  return (c) => {
    if (c.path === `/v1/admin/ai/${path}/drafts` && c.method === 'POST') return json(202, { draftId: DRAFT });
    if (c.path === `/v1/admin/ai/${path}/drafts/${DRAFT}` && c.method === 'GET') return json(200, draft);
    if (c.path === `/v1/admin/ai/${path}/drafts/${DRAFT}/confirm` && c.method === 'POST') return confirm(c);
    return undefined;
  };
}

const confirmBody = (calls: Call[], path: string) => calls.find((c) => c.path === `/v1/admin/ai/${path}/drafts/${DRAFT}/confirm`)?.body;
const choose = (group: string, option: string) => fireEvent.click(within(screen.getByRole('radiogroup', { name: `What to do with “${group}”` })).getByRole('radio', { name: option }));

describe('assist helpers', () => {
  it('maps errors, line reasons and decisions', () => {
    const err = (status: number, code: string, fields?: Record<string, string>) => new ApiError({ type: 'about:blank', title: code, status, code, ...(fields ? { fields } : {}) }, new Headers());
    expect(assistErrorKey(err(429, 'ai_daily_cap_reached'))).toBe('cap');
    expect(assistErrorKey(err(404, 'not_found'))).toBe('off');
    expect(assistErrorKey(err(409, 'invalid_transition'))).toBe('moved');
    expect(assistErrorKey(err(422, 'validation_failed'))).toBe('invalid');
    expect(assistErrorKey(err(403, 'forbidden'))).toBe('forbidden');
    expect(assistErrorKey(err(500, 'internal'))).toBe('generic');
    expect(assistErrorKey(new TypeError('fetch failed'))).toBe('network');
    expect(lineReasons(err(422, 'validation_failed', { 'body.lines[1].value.productId': 'same', 'body.lines[1].outcome': 'other', 'body.x': 'y' }))).toEqual({ 1: 'same' });
    expect(lineReasons(new Error('x'))).toEqual({});
    expect(decision('l1', 'edited', { text: 'a' })).toEqual({ lineId: 'l1', outcome: 'edited', value: { text: 'a' } });
    expect(decision('l1', 'accepted', { text: 'a' })).toEqual({ lineId: 'l1', outcome: 'accepted' });
  });
});

// ---- A-13 risk (AI-A3) ----

function flag(): RiskFlag {
  return {
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
}

function riskApi(): Handler {
  return (c) => {
    if (c.path === '/v1/admin/risk/flags' && c.method === 'GET') return json(200, { items: [flag()], openByRule: [{ rule: 'referral.self_like', open: 1 }], nextCursor: null, serverNow: NOW });
    if (c.path === `/v1/admin/risk/flags/${FLAG_ID}` && c.method === 'GET') {
      return json(200, {
        flag: flag(),
        subject: { type: 'referral', id: REFERRAL_ID, label: 'Sri Lakshmi Stores → Asha', status: 'qualified', storeId: STORE_ID, businessId: null, customerId: CUSTOMER_ID, customerParty: 'referee', businessParty: null },
        facts: [{ key: 'minutes_to_first_order', value: '4' }],
        evidence: [],
        history: [],
        assistEnabled: true,
        serverNow: NOW,
      });
    }
    if (c.path === '/v1/admin/audit-log/subject') return json(200, emptyAudit);
    return undefined;
  };
}

const riskDraft: RiskSummaryDraft = {
  ...base,
  feature: 'risk_summary',
  status: 'ready',
  flagId: FLAG_ID,
  lines: [{ lineId: 'l1', text: 'The first order came 4 minutes after sign-up on a shared device.' }],
};

describe('A-13 risk explanation', () => {
  it('is a draft until confirmed, then records only what was chosen', async () => {
    const { calls } = renderAt(`/risk?flag=${FLAG_ID}`, { roles: ['admin'], handlers: [draftApi('risk-summary', riskDraft), riskApi()] });
    const card = await screen.findByTestId('ai-assist');
    expect(within(card).getByText('AI draft')).toBeInTheDocument();
    expect(calls.some((c) => c.path.startsWith('/v1/admin/ai/'))).toBe(false);
    fireEvent.click(within(card).getByRole('button', { name: 'Explain this flag' }));
    expect(await within(card).findByText(riskDraft.lines[0]?.text ?? '')).toBeInTheDocument();
    const create = calls.find((c) => c.path === '/v1/admin/ai/risk-summary/drafts');
    expect(create?.body).toEqual({ flagId: FLAG_ID });
    expect(create?.headers.get('Idempotency-Key')).toMatch(KEY);
    // Nothing is chosen yet: the line starts as skipped.
    expect(within(card).getByRole('radio', { name: 'Skip' })).toHaveAttribute('aria-checked', 'true');
    choose('Summary', 'Add to audit log');
    fireEvent.click(within(card).getByRole('button', { name: 'Confirm choices' }));
    await waitFor(() => expect(confirmBody(calls, 'risk-summary')).toEqual({ lines: [{ lineId: 'l1', outcome: 'accepted' }] }));
    expect(calls.find((c) => c.path.endsWith('/confirm'))?.headers.get('Idempotency-Key')).toMatch(KEY);
  });

  it('shows an ungrounded edit on the line', async () => {
    const ungrounded = () => json(422, { type: 'about:blank', title: 'x', status: 422, code: 'validation_failed', fields: { 'body.lines[0].value.text': 'ungrounded' } });
    const { calls } = renderAt(`/risk?flag=${FLAG_ID}`, { roles: ['admin'], handlers: [draftApi('risk-summary', riskDraft, ungrounded), riskApi()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Explain this flag' }));
    await screen.findByText(riskDraft.lines[0]?.text ?? '');
    choose('Summary', 'Edit');
    const box = screen.getByRole('textbox', { name: 'Summary' });
    fireEvent.change(box, { target: { value: '  The order came 9 minutes later.  ' } });
    fireEvent.click(screen.getByRole('button', { name: 'Confirm choices' }));
    expect(await screen.findByText("A number in this summary is not in the evidence. Use only the flag's own numbers.")).toBeInTheDocument();
    expect(confirmBody(calls, 'risk-summary')).toEqual({ lines: [{ lineId: 'l1', outcome: 'edited', value: { text: 'The order came 9 minutes later.' } }] });
  });

  it('says when the daily cap is reached', async () => {
    const cap: Handler = (c) => (c.path === '/v1/admin/ai/risk-summary/drafts' ? problem(429, 'ai_daily_cap_reached') : undefined);
    renderAt(`/risk?flag=${FLAG_ID}`, { roles: ['admin'], handlers: [cap, riskApi()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Explain this flag' }));
    expect(await screen.findByText('AI help is paused until tomorrow.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Ask again' })).toBeInTheDocument();
  });
});

// ---- A-14 support (AI-A2) ----

function ticket(): TicketDetail {
  return {
    ticket: {
      id: TID,
      number: 'SUP-001001',
      subject: 'Last week’s payout has not arrived',
      category: 'payouts',
      status: 'open',
      priority: 'normal',
      openerType: 'business',
      opener: { id: OWNER, name: 'Lakshmi' },
      businessId: null,
      businessName: null,
      storeId: null,
      storeName: null,
      orderId: null,
      orderNumber: null,
      assignee: null,
      slaDueAt: NOW,
      overdue: false,
      lastMessageAt: NOW,
      createdAt: NOW,
    },
    order: null,
    firstResponseAt: null,
    resolvedAt: null,
    closedAt: null,
    closeReason: null,
    messages: [],
    agents: [],
    assistEnabled: true,
    serverNow: NOW,
  };
}

const triageDraft: TicketTriageDraft = {
  ...base,
  feature: 'ticket_triage',
  status: 'ready',
  ticketId: TID,
  classify: { lineId: 'l1', category: 'payouts', priority: 'high', orderId: null, orderNumber: null, reason: 'Payout missing for a week.' },
  summary: { lineId: 'l2', text: 'The owner says last week’s payout is not in the bank.' },
  reply: { lineId: 'l3', text: 'Sorry for the wait. We are checking the bank transfer and will update you today.' },
};

describe('A-14 triage', () => {
  it('sets category and priority from an edit and sends the reply, the summary only used or skipped', async () => {
    const supportApi: Handler = (c) => (c.path === `/v1/admin/tickets/${TID}` && c.method === 'GET' ? json(200, ticket()) : undefined);
    const { calls } = renderAt(`/support/${TID}`, { roles: ['support'], handlers: [draftApi('ticket-triage', triageDraft), supportApi] });
    fireEvent.click(await screen.findByRole('button', { name: 'Draft triage' }));
    expect(await screen.findByText(triageDraft.reply?.text ?? '')).toBeInTheDocument();
    expect(calls.find((c) => c.path === '/v1/admin/ai/ticket-triage/drafts')?.body).toEqual({ ticketId: TID });
    // The summary takes no edit.
    expect(within(screen.getByRole('radiogroup', { name: 'What to do with “Thread summary”' })).queryByRole('radio', { name: 'Edit' })).toBeNull();
    choose('Category and priority', 'Edit');
    fireEvent.change(screen.getByRole('combobox', { name: 'Priority' }), { target: { value: 'urgent' } });
    choose('Reply draft', 'Send this reply');
    fireEvent.click(screen.getByRole('button', { name: 'Confirm choices' }));
    await waitFor(() =>
      expect(confirmBody(calls, 'ticket-triage')).toEqual({
        lines: [
          { lineId: 'l1', outcome: 'edited', value: { category: 'payouts', priority: 'urgent' } },
          { lineId: 'l2', outcome: 'discarded' },
          { lineId: 'l3', outcome: 'accepted' },
        ],
      }),
    );
  });
});

// ---- A-08 catalogue (AI-A1) ----

function product(over: Partial<AdminProduct> = {}): AdminProduct {
  return {
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
    ...over,
  };
}

function pending(): AdminPendingProduct {
  return {
    product: product({ id: PENDING, name: 'toor dal unpolished 1kg', brandName: null, variant: null, mrpPaise: null, status: 'pending_review', storeCount: 0 }),
    createdByStoreId: STORE_ID,
    createdByStoreName: 'Annapurna Kirana',
    suggestions: [{ product: product(), similarity: 0.62 }],
  };
}

const catalogueDraft: CatalogueCleanupDraft = {
  ...base,
  feature: 'catalogue_cleanup',
  status: 'ready',
  pendingId: PENDING,
  normalise: { lineId: 'l1', name: 'Toor Dal', brandName: null, variant: 'Unpolished', packSize: 1, unit: 'kg', categoryId: STAPLES, categoryName: 'Staples', canApply: true },
  duplicate: { lineId: 'l2', productId: TOOR, productName: 'Toor Dal', similarity: 0.62, reason: 'Same dal, same pack.', canApply: true },
  categoryNames: { lineId: 'l3', categoryId: STAPLES, names: { en: 'Staples', hi: 'राशन', kn: 'ದಿನಸಿ', te: 'సరుకులు', ta: 'மளிகை' }, canApply: false },
};

function catalogueApi(): Handler {
  return (c) => {
    if (c.path === '/v1/admin/pending-products' && c.method === 'GET') return json(200, { items: [pending()], assistEnabled: true, nextCursor: null, serverNow: NOW });
    if (c.path === '/v1/admin/categories' && c.method === 'GET') return json(200, { items: [] });
    if (c.path === '/v1/admin/audit-log/subject') return json(200, emptyAudit);
    return undefined;
  };
}

describe('A-08 clean-up', () => {
  it('refuses merge and clean-up together, keeps a line the role may not apply skipped, then merges', async () => {
    const { calls } = renderAt(`/catalogue?tab=review&open=${PENDING}`, { roles: ['admin'], handlers: [draftApi('catalogue-cleanup', catalogueDraft), catalogueApi()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Draft a clean-up' }));
    expect(await screen.findByText('Same as Toor Dal (62% name match)')).toBeInTheDocument();
    expect(calls.find((c) => c.path === '/v1/admin/ai/catalogue-cleanup/drafts')?.body).toEqual({ pendingId: PENDING });
    expect(screen.getByText('Your role cannot apply this line. Ask someone who can.')).toBeInTheDocument();
    expect(screen.queryByRole('radiogroup', { name: 'What to do with “Category names”' })).toBeNull();
    choose('Clean fields', 'Use these fields');
    choose('Likely duplicate', 'Merge into it');
    expect(screen.getByText('Merge or clean up, not both: skip one of the two lines.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Confirm choices' })).toBeDisabled();
    choose('Clean fields', 'Skip');
    fireEvent.click(screen.getByRole('button', { name: 'Confirm choices' }));
    await waitFor(() =>
      expect(confirmBody(calls, 'catalogue-cleanup')).toEqual({
        lines: [
          { lineId: 'l1', outcome: 'discarded' },
          { lineId: 'l2', outcome: 'accepted' },
          { lineId: 'l3', outcome: 'discarded' },
        ],
      }),
    );
  });
});
