import { renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { pageVisibility, useRealtimeInvalidation } from './react';

const enc = new TextEncoder();

function mockFetch() {
  const pushes: ((t: string) => void)[] = [];
  const f = vi.fn((_u: string, init: RequestInit) => {
    let ctrl!: ReadableStreamDefaultController<Uint8Array>;
    const body = new ReadableStream<Uint8Array>({
      start(c) {
        ctrl = c;
      },
    });
    (init.signal as AbortSignal).addEventListener('abort', () => {
      try {
        ctrl.error(new DOMException('aborted', 'AbortError'));
      } catch {
        // closed
      }
    });
    pushes.push((t) => ctrl.enqueue(enc.encode(t)));
    return Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, body } as unknown as Response);
  });
  return { f, pushes };
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe('useRealtimeInvalidation', () => {
  it('invalidates mapped keys per event and every reset key on reset', async () => {
    const { f, pushes } = mockFetch();
    const invalidateQueries = vi.fn();
    const { unmount } = renderHook(() =>
      useRealtimeInvalidation(
        {
          url: 'https://api.test/s',
          getToken: () => 't',
          refresh: () => Promise.resolve(true),
          queryClient: { invalidateQueries },
          resetKeys: [['orders', 's1'], ['inbox']],
          fetch: f as unknown as typeof fetch,
        },
        { 'order.created': () => [['orders', 's1']], 'order.updated': (e) => [['order', e.id]] },
      ),
    );
    await vi.advanceTimersByTimeAsync(0);
    pushes[0]?.('event: order.created\nid: 2\ndata: {"type":"order.created","id":"o1","version":1}\n\n');
    pushes[0]?.('event: order.updated\nid: 3\ndata: {"type":"order.updated","id":"o2","version":2}\n\n');
    pushes[0]?.('event: ask.created\nid: 4\ndata: {"type":"ask.created","id":"a","version":1}\n\n');
    await vi.advanceTimersByTimeAsync(0);
    expect(invalidateQueries.mock.calls.map((c) => c[0])).toEqual([
      { queryKey: ['orders', 's1'] },
      { queryKey: ['order', 'o2'] },
    ]);

    invalidateQueries.mockClear();
    pushes[0]?.('event: reset\ndata: {}\n\n');
    await vi.advanceTimersByTimeAsync(0);
    expect(invalidateQueries.mock.calls.map((c) => c[0])).toEqual([
      { queryKey: ['orders', 's1'] },
      { queryKey: ['inbox'] },
    ]);
    unmount();
  });

  it('aborts on unmount and does not connect when disabled', async () => {
    const { f } = mockFetch();
    const base = {
      getToken: () => 't',
      refresh: () => Promise.resolve(true),
      queryClient: { invalidateQueries: vi.fn() },
      resetKeys: [],
      fetch: f as unknown as typeof fetch,
    };
    const off = renderHook(() => useRealtimeInvalidation({ ...base, url: 'u', enabled: false }, {}));
    await vi.advanceTimersByTimeAsync(0);
    expect(f).not.toHaveBeenCalled();
    off.unmount();

    const on = renderHook(() => useRealtimeInvalidation({ ...base, url: 'u' }, {}));
    await vi.advanceTimersByTimeAsync(0);
    const signal = f.mock.calls[0]?.[1].signal as AbortSignal;
    on.unmount();
    expect(signal.aborted).toBe(true);
  });

  it('restarts when the url changes', async () => {
    const { f } = mockFetch();
    const base = {
      getToken: () => 't',
      refresh: () => Promise.resolve(true),
      queryClient: { invalidateQueries: vi.fn() },
      resetKeys: [],
      fetch: f as unknown as typeof fetch,
    };
    const { rerender, unmount } = renderHook(({ url }) => useRealtimeInvalidation({ ...base, url }, {}), {
      initialProps: { url: 'a' },
    });
    await vi.advanceTimersByTimeAsync(0);
    rerender({ url: 'b' });
    await vi.advanceTimersByTimeAsync(0);
    expect(f.mock.calls.map((c) => c[0])).toEqual(['a', 'b']);
    unmount();
  });
});

describe('pageVisibility', () => {
  it('reads document.visibilityState', () => {
    expect(pageVisibility.isVisible()).toBe(true);
    const cb = vi.fn();
    const off = pageVisibility.subscribe(cb);
    document.dispatchEvent(new Event('visibilitychange'));
    expect(cb).toHaveBeenCalledTimes(1);
    off();
    document.dispatchEvent(new Event('visibilitychange'));
    expect(cb).toHaveBeenCalledTimes(1);
  });
});
