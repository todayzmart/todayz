import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { json, problem, renderAt, type Call, type Handler } from './render';

// C-18 profile photo (D-253): register, PUT straight to storage, confirm; remove; the Home avatar.

const TOKEN = 'tok-photo';
const PHOTO = 'https://s3.test/todayz-private/profile/u/1.jpg?X-Amz-Signature=abc';
const UPLOAD = 'https://s3.test/todayz-private/profile/u/1.upload?X-Amz-Signature=put';
const base = { id: '01J9ZAAAAAAAAAAAAAAAAAAAAA', name: 'Asha', preferredLang: 'en', phoneMasked: '90•••••011', app: 'customer', createdAt: '2026-10-01T00:00:00Z' };

/** A signed-in customer whose account the handlers keep, so a reload after a change shows it. */
function photoHandlers(start: string | null, extra: Handler[] = []) {
  let me = { ...base, photoUrl: start };
  const handlers: Handler[] = [
    (c) =>
      c.path === '/v1/auth/refresh'
        ? json(200, { accessToken: TOKEN, expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(), tokenType: 'Bearer', userId: base.id })
        : undefined,
    ...extra,
    (c) => (c.path === '/v1/me' && c.method === 'GET' ? json(200, me, { etag: '"v1"' }) : undefined),
    (c) =>
      c.path === '/v1/me/photo/uploads' && c.method === 'POST'
        ? json(
            201,
            {
              id: '01J9ZBBBBBBBBBBBBBBBBBBBBB',
              upload: {
                url: UPLOAD,
                method: 'PUT',
                expiresAt: '2026-10-26T10:15:00Z',
                headers: { 'Content-Type': (c.body as { contentType: string }).contentType, 'Content-Length': '4' },
              },
            },
            { 'cache-control': 'no-store' },
          )
        : undefined,
    (c) =>
      c.path === '/v1/me/photo/uploads/01J9ZBBBBBBBBBBBBBBBBBBBBB/confirm' && c.method === 'POST'
        ? ((me = { ...me, photoUrl: PHOTO }), json(200, me, { etag: '"v2"' }))
        : undefined,
    (c) => (c.path === '/v1/me/photo' && c.method === 'DELETE' ? ((me = { ...me, photoUrl: null }), json(200, me, { etag: '"v3"' })) : undefined),
  ];
  return handlers;
}

/** The storage PUT goes through the browser's own fetch, not the API session. */
function stubStorage(status = 200) {
  const puts: { url: string; init: RequestInit | undefined }[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
    puts.push({ url: String(input), init });
    return new Response(null, { status });
  });
  return puts;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

function choose(file: File) {
  fireEvent.change(screen.getByTestId('photo-input'), { target: { files: [file] } });
}

const jpeg = () => new File([new Uint8Array([0xff, 0xd8, 0xff, 0xe0])], 'me.jpg', { type: 'image/jpeg' });

describe('C-18 profile photo', () => {
  it('shows the initial, then uploads a photo: register, PUT with only Content-Type, confirm', async () => {
    const puts = stubStorage();
    const { calls } = renderAt('/account', { returning: true, handlers: photoHandlers(null) });
    expect(await screen.findByText('Asha')).toBeInTheDocument();
    const badge = screen.getByRole('button', { name: 'Add a profile photo' });
    expect(document.querySelector('img')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Remove photo' })).toBeNull();

    const input = screen.getByTestId<HTMLInputElement>('photo-input');
    expect(input.accept).toBe('image/jpeg,image/png,image/webp');
    const click = vi.spyOn(input, 'click');
    fireEvent.click(badge);
    expect(click).toHaveBeenCalled();

    await act(async () => choose(jpeg()));
    await screen.findByRole('button', { name: 'Change profile photo' });
    expect(document.querySelector(`img[src="${PHOTO}"]`)).not.toBeNull();
    expect(screen.getByRole('button', { name: 'Remove photo' })).toBeInTheDocument();

    const reg = calls.find((c) => c.path === '/v1/me/photo/uploads');
    expect(reg?.body).toEqual({ contentType: 'image/jpeg', sizeBytes: 4 });
    expect(puts).toHaveLength(1);
    expect(puts[0]?.url).toBe(UPLOAD);
    expect(puts[0]?.init?.method).toBe('PUT');
    expect(puts[0]?.init?.headers).toEqual({ 'Content-Type': 'image/jpeg' });
    expect(calls.some((c: Call) => c.path.endsWith('/confirm'))).toBe(true);
    // The account is reloaded afterwards, so the next name change has the current ETag.
    await waitFor(() => expect(calls.filter((c) => c.path === '/v1/me' && c.method === 'GET').length).toBeGreaterThanOrEqual(2));
  });

  it('refuses another file type before asking the server', async () => {
    stubStorage();
    const { calls } = renderAt('/account', { returning: true, handlers: photoHandlers(null) });
    await screen.findByText('Asha');
    await act(async () => choose(new File(['GIF89a'], 'a.gif', { type: 'image/gif' })));
    expect(await screen.findByRole('alert')).toHaveTextContent('Choose a JPEG, PNG or WebP photo.');
    expect(calls.some((c) => c.path.startsWith('/v1/me/photo'))).toBe(false);
  });

  it('refuses a photo still over 5 MB', async () => {
    stubStorage();
    const { calls } = renderAt('/account', { returning: true, handlers: photoHandlers(null) });
    await screen.findByText('Asha');
    const big = new File([new Uint8Array(5 * 1024 * 1024 + 1)], 'big.png', { type: 'image/png' });
    await act(async () => choose(big));
    expect(await screen.findByRole('alert')).toHaveTextContent('That photo is too big. Choose one under 5 MB.');
    expect(calls.some((c) => c.path.startsWith('/v1/me/photo'))).toBe(false);
  });

  it('says so when the hourly limit is reached, and keeps the initial', async () => {
    stubStorage();
    renderAt('/account', {
      returning: true,
      handlers: photoHandlers(null, [(c) => (c.path === '/v1/me/photo/uploads' ? problem(429, 'rate_limited') : undefined)]),
    });
    await screen.findByText('Asha');
    await act(async () => choose(jpeg()));
    expect(await screen.findByRole('alert')).toHaveTextContent("You've changed your photo many times. Try again later.");
    expect(screen.getByRole('button', { name: 'Add a profile photo' })).toBeInTheDocument();
  });

  it('says so when the server cannot read the photo, or storage refuses the PUT', async () => {
    stubStorage();
    renderAt('/account', {
      returning: true,
      handlers: photoHandlers(null, [(c) => (c.path.endsWith('/confirm') ? problem(422, 'validation_failed') : undefined)]),
    });
    await screen.findByText('Asha');
    await act(async () => choose(jpeg()));
    expect(await screen.findByRole('alert')).toHaveTextContent("We couldn't read that photo. Try another one.");

    vi.unstubAllGlobals();
    stubStorage(403);
    await act(async () => choose(jpeg()));
    expect(await screen.findByRole('alert')).toHaveTextContent("We couldn't update your photo. Try again.");
  });

  it('removes the photo', async () => {
    const { calls } = renderAt('/account', { returning: true, handlers: photoHandlers(PHOTO) });
    await screen.findByText('Asha');
    expect(document.querySelector(`img[src="${PHOTO}"]`)).not.toBeNull();
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Remove photo' }));
    });
    await screen.findByRole('button', { name: 'Add a profile photo' });
    expect(document.querySelector('img')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Remove photo' })).toBeNull();
    expect(calls.some((c) => c.path === '/v1/me/photo' && c.method === 'DELETE')).toBe(true);
  });

  it('falls back to the initial when the signed link no longer loads', async () => {
    renderAt('/account', { returning: true, handlers: photoHandlers(PHOTO) });
    await screen.findByText('Asha');
    const img = document.querySelector(`img[src="${PHOTO}"]`) as HTMLImageElement;
    fireEvent.error(img);
    await waitFor(() => expect(document.querySelector('img')).toBeNull());
    const card = screen.getByRole('region', { name: 'Asha' });
    expect(within(card).getByText('A')).toBeInTheDocument();
  });

  it('shows the photo in the Home header avatar', async () => {
    renderAt('/', { returning: true, handlers: photoHandlers(PHOTO) });
    // The header's account link (the bottom nav has an Account tab too).
    await waitFor(() => expect(document.querySelector(`header a[href="/account"] img[src="${PHOTO}"]`)).not.toBeNull());
  });
});
