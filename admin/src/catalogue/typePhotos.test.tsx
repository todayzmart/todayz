import type { AdminTypeKind } from '@todayz/api-client/gen/model/adminTypeKind';
import type { AdminTypePhoto } from '@todayz/api-client/gen/model/adminTypePhoto';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { json, problem, renderAt, type Handler } from '../test/render';
import { validateCatalogueSearch } from './search';

const STAPLES = '01JCATEG0RY0000000000000S1';
const FIRST = '01JTYPEPH0T0000000000000A1';
const SECOND = '01JTYPEPH0T0000000000000A2';

function photo(over: Partial<AdminTypePhoto> = {}): AdminTypePhoto {
  return {
    id: FIRST,
    categoryId: STAPLES,
    typeKey: 'toor dal',
    status: 'pending',
    rank: 1,
    contentType: 'image/jpeg',
    sizeBytes: 64_000,
    previewUrl: 'https://s3.test/get?sig=t1',
    imageKey: null,
    sourceRef: 'Toor dal in a bowl.jpg',
    sourceUrl: 'https://upload.wikimedia.org/wikipedia/commons/a/a1/Toor_dal_in_a_bowl.jpg',
    pageUrl: 'https://commons.wikimedia.org/wiki/File:Toor_dal_in_a_bowl.jpg',
    licence: 'CC BY-SA 4.0',
    attribution: 'A. Photographer',
    rejectReason: null,
    decidedAt: null,
    createdAt: '2026-10-26T05:00:00Z',
    ...over,
  };
}

function kind(photos: AdminTypePhoto[]): AdminTypeKind {
  const showing = photos.some((p) => p.status === 'approved') ? 3 : 0;
  return { categoryId: STAPLES, categoryName: 'Staples', typeKey: 'toor dal', productCount: 3, showing, samples: ['Tata Sampann Toor Dal 1 kg'], photos };
}

function api(): Handler {
  let photos = [photo(), photo({ id: SECOND, rank: 2, sourceRef: 'Pigeon peas.jpg', attribution: 'B. Grower', previewUrl: 'https://s3.test/get?sig=t2' })];
  return (c) => {
    if (c.path === '/v1/admin/type-photos' && c.method === 'GET') {
      const status = new URLSearchParams(c.search).get('status') ?? 'pending';
      const has = photos.some((p) => p.status === 'approved');
      const items = status === 'approved' ? (has ? [kind(photos.filter((p) => p.status === 'approved'))] : []) : has ? [] : [kind(photos)];
      return json(200, { items, nextCursor: null });
    }
    const one = /^\/v1\/admin\/type-photos\/(\w+)\/(approve|reject)$/.exec(c.path);
    if (one && c.method === 'POST') {
      const found = photos.find((p) => p.id === one[1]);
      if (!found) return problem(409, 'invalid_transition');
      if (one[2] === 'approve') {
        photos = photos.map((p) => (p.id === found.id ? { ...p, status: 'approved', imageKey: `types/${p.id}.jpg` } : { ...p, status: 'rejected' }));
        return json(200, { photo: { ...found, status: 'approved' }, products: 3 });
      }
      photos = photos.map((p) => (p.id === found.id ? { ...p, status: 'rejected' } : p));
      return json(200, { photo: { ...found, status: 'rejected' }, products: found.status === 'approved' ? 3 : 0 });
    }
    return undefined;
  };
}

describe('A-08 item photos (D-253)', () => {
  it('keeps the tab and its filter in the URL', () => {
    expect(validateCatalogueSearch({ tab: 'types', photos: 'approved' })).toEqual({ tab: 'types', photos: 'approved' });
    expect(validateCatalogueSearch({ tab: 'types', photos: 'rejected' })).toEqual({ tab: 'types' });
  });

  it('shows a kind with its candidates, credit and Commons page, and picks one', async () => {
    const { calls } = renderAt('/catalogue?tab=types', { roles: ['catalogue'], handlers: [api()] });
    const card = await screen.findByRole('region', { name: 'toor dal' });
    expect(within(card).getByText(/Staples · 3 products/)).toBeInTheDocument();
    expect(within(card).getByText('For example: Tata Sampann Toor Dal 1 kg')).toBeInTheDocument();
    expect(within(card).getByRole('img', { name: 'Candidate 1 for toor dal' })).toHaveAttribute('src', 'https://s3.test/get?sig=t1');
    expect(within(card).getByText(/CC BY-SA 4.0 · A. Photographer/)).toBeInTheDocument();
    expect(within(card).getAllByRole('link', { name: 'Commons page' })[0]).toHaveAttribute('href', 'https://commons.wikimedia.org/wiki/File:Toor_dal_in_a_bowl.jpg');

    const [use] = within(card).getAllByRole('button', { name: 'Use this photo' });
    if (!use) throw new Error('no pick button');
    fireEvent.click(use);
    const dialog = await screen.findByRole('dialog', { name: 'Use this photo for toor dal?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Use this photo' }));
    await waitFor(() => expect(screen.getByText('Photo in use for toor dal on 3 products')).toBeInTheDocument());
    expect(calls.find((c) => c.path === `/v1/admin/type-photos/${FIRST}/approve`)?.body).toEqual({});
    await waitFor(() => expect(screen.getByText('No kinds of item are waiting for a photo.')).toBeInTheDocument());
  });

  it('turns a candidate down only with a reason', async () => {
    const { calls } = renderAt('/catalogue?tab=types', { roles: ['catalogue'], handlers: [api()] });
    const card = await screen.findByRole('region', { name: 'toor dal' });
    const [, second] = within(card).getAllByRole('button', { name: 'Not right' });
    if (!second) throw new Error('no second candidate');
    fireEvent.click(second);
    const dialog = await screen.findByRole('dialog', { name: 'Turn this photo down for toor dal?' });
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'A field, not the dal' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Not right' }));
    await waitFor(() => expect(screen.getByText('Photo turned down')).toBeInTheDocument());
    expect(calls.find((c) => c.path === `/v1/admin/type-photos/${SECOND}/reject`)?.body).toEqual({ reason: 'A field, not the dal' });
  });

  it('lists photos in use and offers to take one down', async () => {
    const approved: Handler = (c) =>
      c.path === '/v1/admin/type-photos' ? json(200, { items: [kind([photo({ status: 'approved', imageKey: `types/${FIRST}.jpg` })])], nextCursor: null }) : undefined;
    const viewer = renderAt('/catalogue?tab=types&photos=approved', { roles: ['catalogue'], handlers: [approved] });
    const card = await screen.findByRole('region', { name: 'toor dal' });
    expect(within(card).getByText('In use')).toBeInTheDocument();
    expect(within(card).getByRole('img', { name: 'Candidate 1 for toor dal' })).toHaveAttribute('src', `https://img.todayz.localhost/types/${FIRST}.jpg`);
    expect(within(card).getByRole('button', { name: 'Take down' })).toBeInTheDocument();
    expect(viewer.calls.find((c) => c.path === '/v1/admin/type-photos')?.search).toContain('status=approved');
  });
});
