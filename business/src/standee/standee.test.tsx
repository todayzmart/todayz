import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { json, problem, renderAt, type Call, type Handler } from '../test/render';

const messages = asMessages(en);
const B = '/v1/biz/businesses/b1';

function shops(role: string): Handler {
  return (c: Call) =>
    c.path === '/v1/biz/businesses'
      ? json(200, { items: [{ id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role, stores: [{ id: 's1', name: 'Indiranagar', slug: 'indiranagar', status: 'live' }] }], invitations: [], nextCursor: null })
      : undefined;
}
const store: Handler = (c) => (c.method === 'GET' && c.path === `${B}/stores/s1` ? json(200, { id: 's1', shortCode: 'K7Q2X9' }) : undefined);
const pdf: Handler = (c) => (c.path === `${B}/stores/s1/qr-standee.pdf` ? new Response(new Blob(['%PDF']), { status: 200, headers: { 'content-type': 'application/pdf' } }) : undefined);

afterEach(() => vi.restoreAllMocks());

describe('standee page', () => {
  it('a cashier gets the no-access state and no PDF request', async () => {
    const { calls } = renderAt('/standee', { messages, handlers: [shops('cashier'), store, pdf] });
    expect(await screen.findByText('The standee is for owners and managers')).toBeInTheDocument();
    expect(calls.some((c) => c.path.endsWith('.pdf'))).toBe(false);
  });

  it('shows the short code and previews the PDF in a frame, fetched with the session token', async () => {
    Object.defineProperty(URL, 'createObjectURL', { value: vi.fn(() => 'blob:standee'), configurable: true });
    Object.defineProperty(URL, 'revokeObjectURL', { value: vi.fn(), configurable: true });
    const { calls } = renderAt('/standee', { messages, handlers: [shops('manager'), store, pdf] });
    expect(await screen.findByText('K7Q2X9')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Preview' }));
    const frame = await screen.findByTitle('Standee preview');
    expect(frame).toHaveAttribute('src', 'blob:standee');
    const req = calls.find((c) => c.path.endsWith('/qr-standee.pdf'));
    if (!req) throw new Error('no standee request');
    expect(req.headers.get('Authorization')).toBe('Bearer test-token');
    expect(req.search).not.toContain('token');
  });

  it('says so, with a retry, when the PDF cannot be made', async () => {
    renderAt('/standee', { messages, handlers: [shops('owner'), store, (c) => (c.path.endsWith('.pdf') ? problem(503, 'service_unavailable') : undefined)] });
    fireEvent.click(await screen.findByRole('button', { name: 'Download A5 PDF' }));
    expect(await screen.findByText('Could not prepare the standee. Try again.')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument());
  });
});
