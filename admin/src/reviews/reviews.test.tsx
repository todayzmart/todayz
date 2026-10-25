import type { AdminReview } from '@todayz/api-client/gen/model/adminReview';
import type { AdminReviewReport } from '@todayz/api-client/gen/model/adminReviewReport';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NOW, ORDER_ID, STORE_ID, emptyAudit } from '../test/fixtures';
import { json, problem, renderAt, type Handler } from '../test/render';
import { validateReviewsSearch } from './search';

const REVIEW_ID = '01JREV1EW00000000000000V01';
const REPORT_ID = '01JREP0RT00000000000000P01';

function review(over: Partial<AdminReview> = {}): AdminReview {
  return {
    id: REVIEW_ID,
    storeId: STORE_ID,
    storeName: 'Sri Lakshmi Stores',
    customerId: '01JCUST0000000000000000C01',
    orderId: ORDER_ID,
    posSaleId: null,
    stars: 1,
    text: 'Rude at the counter and the rice was stale.',
    tags: ['service'],
    createdAt: '2026-10-24T10:00:00Z',
    hidden: false,
    hiddenAt: null,
    hideReason: null,
    openReports: 1,
    reply: { text: 'Sorry, we will check.', createdAt: '2026-10-24T12:00:00Z', hidden: false, hideReason: null },
    ...over,
  };
}

function report(over: Partial<AdminReviewReport> = {}): AdminReviewReport {
  return {
    id: REPORT_ID,
    reviewId: REVIEW_ID,
    reporterKind: 'store',
    reason: 'Abusive language',
    status: 'open',
    resolution: null,
    resolutionNote: null,
    createdAt: '2026-10-24T13:00:00Z',
    resolvedAt: null,
    ...over,
  };
}

function api(start = review()): Handler {
  let current = start;
  let open = report();
  return (c) => {
    if (c.path === '/v1/admin/reviews' && c.method === 'GET') return json(200, { items: [current], nextCursor: null, serverNow: NOW });
    if (c.path === `/v1/admin/reviews/${REVIEW_ID}/hide` && c.method === 'POST') {
      if (current.hidden) return problem(409, 'invalid_transition');
      current = { ...current, hidden: true, hiddenAt: NOW, hideReason: (c.body as { reason: string }).reason, openReports: 0 };
      return json(200, current);
    }
    if (c.path === `/v1/admin/reviews/${REVIEW_ID}/restore` && c.method === 'POST') {
      current = { ...current, hidden: false, hiddenAt: null, hideReason: null };
      return json(200, current);
    }
    if (c.path === '/v1/admin/review-reports' && c.method === 'GET') {
      const status = new URLSearchParams(c.search).get('status');
      return json(200, { items: open.status === status ? [open] : [], nextCursor: null, serverNow: NOW });
    }
    if (c.path === `/v1/admin/review-reports/${REPORT_ID}/resolve` && c.method === 'POST') {
      const b = c.body as { resolution: 'dismissed' | 'upheld'; note?: string };
      open = { ...open, status: 'resolved', resolution: b.resolution, resolutionNote: b.note ?? null, resolvedAt: NOW };
      return json(200, open);
    }
    if (c.path === '/v1/admin/audit-log/subject') return json(200, emptyAudit);
    return undefined;
  };
}

describe('reviews search', () => {
  it('keeps only well-formed filters', () => {
    expect(validateReviewsSearch({ tab: 'reports', state: 'hidden', status: 'resolved', storeId: STORE_ID, review: 'x', y: 1 })).toEqual({
      tab: 'reports',
      state: 'hidden',
      status: 'resolved',
      storeId: STORE_ID,
    });
    expect(validateReviewsSearch({ tab: 'other', state: 'deleted', status: 'all' })).toEqual({});
  });
});

describe('A-12 reviews', () => {
  it('lists reported reviews first and puts the filters in the URL', async () => {
    const { calls, router } = renderAt('/reviews', { roles: ['support'], handlers: [api()] });
    const table = await screen.findByRole('table', { name: 'Reviews' });
    expect(await within(table).findByText('Sri Lakshmi Stores')).toBeInTheDocument();
    expect(new URLSearchParams(calls.find((c) => c.path === '/v1/admin/reviews')?.search).get('state')).toBe('reported');
    const form = screen.getByRole('search', { name: 'Review filters' });
    fireEvent.change(within(form).getByLabelText('Show'), { target: { value: 'hidden' } });
    fireEvent.change(within(form).getByLabelText('Store ID'), { target: { value: STORE_ID.toLowerCase() } });
    fireEvent.click(within(form).getByRole('button', { name: 'Apply' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ state: 'hidden', storeId: STORE_ID }));
    await waitFor(() => expect(calls.filter((c) => c.path === '/v1/admin/reviews')).toHaveLength(2));
    const q = new URLSearchParams(calls.filter((c) => c.path === '/v1/admin/reviews')[1]?.search);
    expect(q.get('state')).toBe('hidden');
    expect(q.get('storeId')).toBe(STORE_ID);
  });

  it('hides with a required reason, then restores with one', async () => {
    const { calls, router } = renderAt('/reviews', { roles: ['support'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: /Rude at the counter/ }));
    await waitFor(() => expect(router.state.location.search).toEqual({ review: REVIEW_ID }));
    expect(await screen.findByRole('heading', { name: 'Review of Sri Lakshmi Stores' })).toBeInTheDocument();
    expect(screen.getByRole('complementary', { name: 'History' })).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Hide review' }));
    let dialog = await screen.findByRole('dialog', { name: 'Hide this review of Sri Lakshmi Stores?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Hide review' }));
    expect(await within(dialog).findByText(/Give a reason/)).toBeInTheDocument();
    expect(calls.some((c) => c.path.endsWith('/hide'))).toBe(false);
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: '  abusive language  ' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Hide review' }));
    await waitFor(() => expect(screen.getByText('Review hidden.')).toBeInTheDocument());
    expect(calls.find((c) => c.path === `/v1/admin/reviews/${REVIEW_ID}/hide`)?.body).toEqual({ reason: 'abusive language' });
    expect(await screen.findByText('Hidden because: abusive language')).toBeInTheDocument();

    fireEvent.click(await screen.findByRole('button', { name: 'Restore review' }));
    dialog = await screen.findByRole('dialog', { name: 'Restore this review of Sri Lakshmi Stores?' });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'appeal accepted' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Restore review' }));
    await waitFor(() => expect(screen.getByText('Review restored.')).toBeInTheDocument());
    expect(calls.find((c) => c.path === `/v1/admin/reviews/${REVIEW_ID}/restore`)?.body).toEqual({ reason: 'appeal accepted' });
  });

  it('shows the 409 when the review was already hidden elsewhere', async () => {
    const stale = review();
    const handler: Handler = (c) => {
      if (c.path === `/v1/admin/reviews/${REVIEW_ID}/hide`) return problem(409, 'invalid_transition');
      return api(stale)(c);
    };
    renderAt(`/reviews?review=${REVIEW_ID}`, { roles: ['admin'], handlers: [handler] });
    fireEvent.click(await screen.findByRole('button', { name: 'Hide review' }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'spam' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Hide review' }));
    expect(await within(dialog).findByRole('alert')).toBeInTheDocument();
  });

  it('resolves a report with a decision and an optional note', async () => {
    const { calls } = renderAt('/reviews?tab=reports', { roles: ['support'], handlers: [api()] });
    const table = await screen.findByRole('table', { name: 'Review reports' });
    expect(await within(table).findByText('Abusive language')).toBeInTheDocument();
    fireEvent.click(within(table).getByRole('button', { name: 'Resolve' }));
    const dialog = await screen.findByRole('dialog', { name: 'Resolve report' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Resolve report' }));
    expect(await within(dialog).findByText('Choose dismissed or upheld.')).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole('radio', { name: 'Dismissed' }));
    fireEvent.change(within(dialog).getByLabelText('Note (optional)'), { target: { value: 'fair criticism' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Resolve report' }));
    await waitFor(() => expect(screen.getByText('Report resolved.')).toBeInTheDocument());
    expect(calls.find((c) => c.path === `/v1/admin/review-reports/${REPORT_ID}/resolve`)?.body).toEqual({ resolution: 'dismissed', note: 'fair criticism' });
    expect(await screen.findByText('No open reports. Nothing to moderate.')).toBeInTheDocument();
  });

  it('offers the reply moves only when the shop replied', async () => {
    renderAt(`/reviews?review=${REVIEW_ID}`, { roles: ['super_admin'], handlers: [api(review({ reply: null }))] });
    expect(await screen.findByRole('button', { name: 'Hide review' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Hide reply' })).not.toBeInTheDocument();
  });
});
