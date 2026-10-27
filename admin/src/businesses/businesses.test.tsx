import type { AdminBusiness } from '@todayz/api-client/gen/model/adminBusiness';
import type { AdminLead } from '@todayz/api-client/gen/model/adminLead';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NOW, STORE_ID, emptyAudit } from '../test/fixtures';
import { json, problem, renderAt, type Handler } from '../test/render';
import { validateBusinessesSearch } from './search';

const BUSINESS_ID = '01JBUS1NESS0000000000000B1';
const DOC_ID = '01JD0C000000000000000000D1';
const LEAD_ID = '01JLEAD0000000000000000L01';

function business(over: Partial<AdminBusiness> = {}): AdminBusiness {
  return {
    id: BUSINESS_ID,
    brandName: 'Sri Lakshmi Stores',
    legalName: 'Sri Lakshmi Traders',
    status: 'submitted',
    needsInfoFields: [],
    needsInfoNote: null,
    submittedAt: '2026-10-24T05:00:00Z',
    verifiedAt: null,
    checkStatus: 'unchecked',
    checkedAt: null,
    checkNote: null,
    createdAt: '2026-10-20T05:00:00Z',
    kyc: {
      status: 'submitted',
      legalName: 'Sri Lakshmi Traders',
      panLast4: '234F',
      gstinLast4: '1Z5Q',
      upiLast4: null,
      bankLast4: '6789',
      bankIfsc: 'HDFC0001234',
      notes: null,
      verifiedAt: null,
    },
    stores: [
      { id: STORE_ID, name: 'Lakshmi, Jayanagar', status: 'pending_review', locality: 'Jayanagar', city: 'Bengaluru', liveAt: null, createdAt: '2026-10-21T05:00:00Z' },
    ],
    staff: [{ id: '01JSTAFF000000000000000F01', name: 'Ravi', role: 'owner', status: 'active', phoneMasked: '98•••••321' }],
    documents: [
      {
        id: DOC_ID,
        kind: 'gst',
        contentType: 'application/pdf',
        sizeBytes: 120_000,
        storeId: null,
        status: 'uploaded',
        rejectReason: null,
        createdAt: '2026-10-22T05:00:00Z',
        reviewedAt: null,
      },
    ],
    serverNow: NOW,
    ...over,
  };
}

function lead(over: Partial<AdminLead> = {}): AdminLead {
  return {
    id: LEAD_ID,
    name: 'Kiran',
    shopName: 'Kiran Medicals',
    city: 'Mysuru',
    source: 'site',
    phoneMasked: '97•••••456',
    status: 'new',
    statusChangedAt: null,
    createdAt: '2026-10-24T09:00:00Z',
    ...over,
  };
}

function api(start = business()): Handler {
  let current = start;
  let l = lead();
  const base = `/v1/admin/businesses/${BUSINESS_ID}`;
  return (c) => {
    if (c.path === '/v1/admin/businesses' && c.method === 'GET') {
      const { documents: _d, staff: _s, stores, kyc: _k, needsInfoFields: _f, needsInfoNote: _n, verifiedAt: _v, serverNow: _now, ...row } = current;
      return json(200, { items: [{ ...row, city: 'Bengaluru', storeCount: stores.length }], nextCursor: null, serverNow: NOW });
    }
    if (c.path === base && c.method === 'GET') return json(200, current);
    if (c.path === `${base}/verify`) {
      if (current.status !== 'submitted') return problem(409, 'invalid_transition');
      current = { ...current, status: 'verified', verifiedAt: NOW };
      return json(200, current);
    }
    if (c.path === `${base}/request-info`) {
      const b = c.body as { fields: string[]; note: string };
      current = { ...current, status: 'needs_info', needsInfoFields: b.fields, needsInfoNote: b.note };
      return json(200, current);
    }
    if (c.path === `${base}/kyc/reveal`) {
      const { field } = c.body as { field: string };
      return json(200, { field, value: field === 'pan' ? 'ABCPL1234F' : 'x' }, { 'cache-control': 'no-store' });
    }
    if (c.path === `${base}/documents/${DOC_ID}/review`) {
      const b = c.body as { decision: 'accepted' | 'rejected'; reason?: string };
      current = {
        ...current,
        documents: current.documents.map((d) => ({ ...d, status: b.decision, rejectReason: b.reason ?? null, reviewedAt: NOW })),
      };
      return json(200, current);
    }
    if (c.path === '/v1/admin/leads' && c.method === 'GET') return json(200, { items: [l], nextCursor: null, serverNow: NOW });
    if (c.path === `/v1/admin/leads/${LEAD_ID}/status`) {
      const b = c.body as { status: AdminLead['status'] };
      l = { ...l, status: b.status, statusChangedAt: NOW };
      return json(200, l);
    }
    if (c.path === '/v1/admin/audit-log/subject') return json(200, emptyAudit);
    return undefined;
  };
}

describe('businesses search', () => {
  it('keeps only well-formed filters', () => {
    expect(validateBusinessesSearch({ status: 'needs_info', x: 1 })).toEqual({ status: 'needs_info' });
    expect(validateBusinessesSearch({ tab: 'leads', lead: 'contacted' })).toEqual({ tab: 'leads', lead: 'contacted' });
    expect(validateBusinessesSearch({ tab: 'other', status: 'draft', lead: 'won' })).toEqual({});
  });
});

describe('A-03 businesses', () => {
  it('lists the submitted queue for support and links to the detail', async () => {
    const { calls } = renderAt('/businesses', { roles: ['support'], handlers: [api()] });
    const table = await screen.findByRole('table', { name: 'Businesses: Submitted' });
    expect(await within(table).findByRole('link', { name: 'Sri Lakshmi Stores' })).toHaveAttribute('href', `/businesses/${BUSINESS_ID}`);
    expect(new URLSearchParams(calls.find((c) => c.path === '/v1/admin/businesses')?.search).get('status')).toBe('submitted');
  });

  it('shows a field agent only the Leads tab, with masked phones', async () => {
    const { calls } = renderAt('/businesses', { roles: ['field_agent'], handlers: [api()] });
    const table = await screen.findByRole('table', { name: 'Leads' });
    expect(await within(table).findByText('97•••••456')).toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: 'Submitted' })).not.toBeInTheDocument();
    expect(calls.some((c) => c.path === '/v1/admin/businesses')).toBe(false);
  });

  it('moves a lead with an optional note', async () => {
    const { calls } = renderAt('/businesses?tab=leads', { roles: ['field_agent'], handlers: [api()] });
    fireEvent.change(await screen.findByLabelText('Status of Kiran Medicals'), { target: { value: 'contacted' } });
    const dialog = await screen.findByRole('dialog', { name: 'Mark Kiran Medicals as Contacted?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save status' }));
    await waitFor(() => expect(screen.getByText('Lead updated')).toBeInTheDocument());
    expect(calls.find((c) => c.path === `/v1/admin/leads/${LEAD_ID}/status`)?.body).toEqual({ status: 'contacted' });
  });

  it('verifies a submitted business', async () => {
    const { calls } = renderAt(`/businesses/${BUSINESS_ID}`, { roles: ['admin'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Verify' }));
    const dialog = await screen.findByRole('dialog', { name: 'Verify Sri Lakshmi Stores?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Verify' }));
    await waitFor(() => expect(screen.getByText('Business verified')).toBeInTheDocument());
    expect(calls.find((c) => c.path.endsWith('/verify'))?.body).toEqual({});
    expect(screen.getByRole('complementary', { name: 'History' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Verify' })).not.toBeInTheDocument();
  });

  it('asks for info only with a field picked and a note', async () => {
    const { calls } = renderAt(`/businesses/${BUSINESS_ID}`, { roles: ['admin'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Ask for info' }));
    const dialog = await screen.findByRole('dialog', { name: 'Ask Sri Lakshmi Stores for details?' });
    expect(within(dialog).getByText('Pick at least one.')).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText('Note to the owner'), { target: { value: 'Upload a clear GST certificate' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Ask for info' }));
    expect(calls.some((c) => c.path.endsWith('/request-info'))).toBe(false);
    fireEvent.click(within(dialog).getByLabelText('Documents'));
    fireEvent.click(within(dialog).getByRole('button', { name: 'Ask for info' }));
    await waitFor(() => expect(screen.getByText('Sent back to the owner')).toBeInTheDocument());
    expect(calls.find((c) => c.path.endsWith('/request-info'))?.body).toEqual({ fields: ['documents'], note: 'Upload a clear GST certificate' });
  });

  it('reveals a KYC value on request and hides it again; support cannot reveal', async () => {
    const { calls, unmount } = renderAt(`/businesses/${BUSINESS_ID}`, { roles: ['admin'], handlers: [api()] });
    expect(await screen.findByText('•••• 234F')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Show PAN in full' }));
    expect(await screen.findByText('ABCPL1234F')).toBeInTheDocument();
    expect(calls.find((c) => c.path.endsWith('/kyc/reveal'))?.body).toEqual({ field: 'pan' });
    fireEvent.click(screen.getByRole('button', { name: 'Hide PAN' }));
    expect(screen.queryByText('ABCPL1234F')).not.toBeInTheDocument();
    expect(screen.getByText('•••• 234F')).toBeInTheDocument();
    unmount();

    renderAt(`/businesses/${BUSINESS_ID}`, { roles: ['support'], handlers: [api()] });
    expect(await screen.findByText('•••• 234F')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Show PAN in full' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Verify' })).not.toBeInTheDocument();
  });

  it('accepts a document directly and rejects one only with a reason', async () => {
    const { calls } = renderAt(`/businesses/${BUSINESS_ID}`, { roles: ['admin'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('button', { name: 'Reject GST certificate' }));
    const dialog = await screen.findByRole('dialog', { name: 'Reject the GST certificate?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Reject' }));
    expect(await within(dialog).findByText(/Write a reason/)).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'Blurred' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Reject' }));
    await waitFor(() => expect(screen.getByText('Document review saved')).toBeInTheDocument());
    expect(calls.find((c) => c.path.endsWith('/review'))?.body).toEqual({ decision: 'rejected', reason: 'Blurred' });

    fireEvent.click(screen.getByRole('button', { name: 'Accept GST certificate' }));
    await waitFor(() => expect(calls.filter((c) => c.path.endsWith('/review'))).toHaveLength(2));
    expect(calls.filter((c) => c.path.endsWith('/review'))[1]?.body).toEqual({ decision: 'accepted' });
  });
});
