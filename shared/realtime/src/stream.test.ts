import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createRealtimeStream, type RealtimeEvent, type RealtimeStatus } from './stream';

const enc = new TextEncoder();

interface Conn {
  headers: Record<string, string>;
  push(text: string): void;
  end(): void;
  signal: AbortSignal;
}

type Step = { status: number; headers?: Record<string, string> } | { throws: true };

function setup(
  opts: { token?: string | null; refresh?: () => Promise<boolean>; visible?: { v: boolean }; plan?: Step[] } = {},
) {
  const conns: Conn[] = [];
  const plan: Step[] = [...(opts.plan ?? [])];
  const fetchMock = vi.fn((_url: string, init: RequestInit) => {
    const step = plan.shift();
    if (step && 'throws' in step) return Promise.reject(new TypeError('network'));
    if (step && step.status !== 200) {
      return Promise.resolve({
        ok: false,
        status: step.status,
        headers: { get: (k: string) => step.headers?.[k] ?? null },
        body: null,
      } as unknown as Response);
    }
    let ctrl!: ReadableStreamDefaultController<Uint8Array>;
    const body = new ReadableStream<Uint8Array>({
      start(c) {
        ctrl = c;
      },
    });
    const signal = init.signal as AbortSignal;
    signal.addEventListener('abort', () => {
      try {
        ctrl.error(new DOMException('aborted', 'AbortError'));
      } catch {
        // already closed
      }
    });
    conns.push({
      headers: init.headers as Record<string, string>,
      push: (t) => ctrl.enqueue(enc.encode(t)),
      end: () => ctrl.close(),
      signal,
    });
    return Promise.resolve({
      ok: true,
      status: 200,
      headers: { get: () => null },
      body,
    } as unknown as Response);
  });
  const events: RealtimeEvent[] = [];
  const statuses: RealtimeStatus[] = [];
  const onReset = vi.fn();
  const poll = vi.fn();
  const refresh = opts.refresh ?? vi.fn(() => Promise.resolve(true));
  const vis = opts.visible ?? { v: true };
  const visCbs = new Set<() => void>();
  const stream = createRealtimeStream({
    url: 'https://api.test/v1/me/stream',
    getToken: () => (opts.token === undefined ? 'tok' : opts.token),
    refresh,
    fetch: fetchMock as unknown as typeof fetch,
    onEvent: (e) => events.push(e),
    onReset,
    onStatus: (s) => statuses.push(s),
    poll,
    visibility: {
      isVisible: () => vis.v,
      subscribe: (cb) => {
        visCbs.add(cb);
        return () => visCbs.delete(cb);
      },
    },
    timers: {
      setTimeout: (fn, ms) => setTimeout(fn, ms),
      clearTimeout: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
      now: () => Date.now(),
      random: () => 1,
    },
  });
  const setVisible = (v: boolean) => {
    vis.v = v;
    visCbs.forEach((cb) => cb());
  };
  return { conns, plan, fetchMock, events, statuses, onReset, poll, refresh, stream, setVisible };
}

const flush = () => vi.advanceTimersByTimeAsync(0);

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

const ready = 'retry: 3000\n\nevent: ready\nid: 1\ndata: {"resumed":false}\n\n';

describe('createRealtimeStream', () => {
  it('connects with the bearer token and dispatches events', async () => {
    const s = setup();
    await flush();
    expect(s.conns[0]?.headers.Authorization).toBe('Bearer tok');
    expect(s.conns[0]?.headers['Last-Event-ID']).toBeUndefined();
    s.conns[0]?.push(ready);
    s.conns[0]?.push('event: order.created\nid: 2\ndata: {"type":"order.created","id":"o1","version":3}\n\n');
    await flush();
    expect(s.statuses).toEqual(['connecting', 'open']);
    expect(s.events).toEqual([{ type: 'order.created', id: 'o1', version: 3 }]);
    expect(s.onReset).not.toHaveBeenCalled();
    s.stream.stop();
  });

  it('resumes with Last-Event-ID after a reconnect frame and does not count it as a failure', async () => {
    const s = setup();
    await flush();
    s.conns[0]?.push(ready);
    s.conns[0]?.push(': hb\n\nid: 7\n\n');
    s.conns[0]?.push('retry: 1000\nevent: reconnect\ndata: {"reason":"shutdown"}\n\n');
    s.conns[0]?.end();
    await flush();
    expect(s.conns).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(1000);
    expect(s.conns).toHaveLength(2);
    expect(s.conns[1]?.headers['Last-Event-ID']).toBe('7');
    // Repeated reconnects never reach polling.
    for (let i = 0; i < 4; i++) {
      s.conns[s.conns.length - 1]?.push('retry: 1000\nevent: reconnect\ndata: {}\n\n');
      s.conns[s.conns.length - 1]?.end();
      await vi.advanceTimersByTimeAsync(1000);
    }
    expect(s.poll).not.toHaveBeenCalled();
    expect(s.statuses).not.toContain('polling');
    s.stream.stop();
  });

  it('calls onReset when the server sends reset', async () => {
    const s = setup();
    await flush();
    s.conns[0]?.push('event: reset\ndata: {}\n\n');
    await flush();
    expect(s.onReset).toHaveBeenCalledTimes(1);
    s.stream.stop();
  });

  it('treats a clean end without reconnect as a failure, then polls after 3, only while visible', async () => {
    const s = setup();
    await flush();
    // failure 1
    s.conns[0]?.end();
    await vi.advanceTimersByTimeAsync(3000);
    expect(s.conns).toHaveLength(2);
    // failure 2 (backoff 6s)
    s.conns[1]?.end();
    await flush();
    await vi.advanceTimersByTimeAsync(6000);
    expect(s.conns).toHaveLength(3);
    expect(s.poll).not.toHaveBeenCalled();
    // failure 3 -> polling
    s.conns[2]?.end();
    await flush();
    expect(s.statuses.at(-1)).toBe('polling');
    expect(s.poll).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(15_000);
    expect(s.poll).toHaveBeenCalledTimes(2);

    // Hidden: no polling.
    s.setVisible(false);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(s.poll).toHaveBeenCalledTimes(2);
    // Becoming visible polls once right away.
    s.setVisible(true);
    expect(s.poll).toHaveBeenCalledTimes(3);
    await vi.advanceTimersByTimeAsync(15_000);
    expect(s.poll).toHaveBeenCalledTimes(4);
    s.stream.stop();
  });

  it('stops polling when a later connection reports ready', async () => {
    const s = setup({ plan: [{ throws: true }, { throws: true }, { throws: true }] });
    await vi.advanceTimersByTimeAsync(3000 + 6000);
    expect(s.statuses.at(-1)).toBe('polling');
    expect(s.poll).toHaveBeenCalledTimes(1);
    // Failure 3 backs off 12 s; the stream keeps trying in the background.
    await vi.advanceTimersByTimeAsync(12_000);
    expect(s.conns).toHaveLength(1);
    s.conns[0]?.push(ready);
    await flush();
    expect(s.statuses.at(-1)).toBe('open');
    const before = s.poll.mock.calls.length;
    await vi.advanceTimersByTimeAsync(60_000);
    expect(s.poll.mock.calls.length).toBe(before);
    s.stream.stop();
  });

  it('refreshes on 401 and retries without counting a failure', async () => {
    const s = setup({ plan: [{ status: 401 }] });
    await flush();
    expect(s.refresh).toHaveBeenCalledTimes(1);
    expect(s.fetchMock).toHaveBeenCalledTimes(2);
    expect(s.conns).toHaveLength(1);
    s.stream.stop();
  });

  it('counts a 401 as a failure when the refresh fails', async () => {
    const refresh = vi.fn(() => Promise.resolve(false));
    const s = setup({ refresh, plan: [{ status: 401 }] });
    await flush();
    expect(s.fetchMock).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(3000);
    expect(s.fetchMock).toHaveBeenCalledTimes(2);
    s.stream.stop();
  });

  it('stops on 403 and 404 without polling', async () => {
    for (const status of [403, 404]) {
      const s = setup({ plan: [{ status }] });
      await flush();
      expect(s.statuses.at(-1)).toBe('stopped');
      await vi.advanceTimersByTimeAsync(120_000);
      expect(s.fetchMock).toHaveBeenCalledTimes(1);
      expect(s.poll).not.toHaveBeenCalled();
    }
  });

  it('honours Retry-After on 429', async () => {
    const s = setup({ plan: [{ status: 429, headers: { 'Retry-After': '20' } }] });
    await flush();
    await vi.advanceTimersByTimeAsync(19_000);
    expect(s.fetchMock).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1000);
    expect(s.fetchMock).toHaveBeenCalledTimes(2);
    s.stream.stop();
  });

  it('aborts and reconnects when nothing arrives for 60 s, counting a failure', async () => {
    const s = setup();
    await flush();
    s.conns[0]?.push(ready);
    await flush();
    await vi.advanceTimersByTimeAsync(59_000);
    expect(s.conns[0]?.signal.aborted).toBe(false);
    await vi.advanceTimersByTimeAsync(1000);
    expect(s.conns[0]?.signal.aborted).toBe(true);
    await vi.advanceTimersByTimeAsync(3000);
    expect(s.conns).toHaveLength(2);
    expect(s.conns[1]?.headers['Last-Event-ID']).toBe('1');
    s.stream.stop();
  });

  it('a heartbeat keeps the watchdog quiet', async () => {
    const s = setup();
    await flush();
    s.conns[0]?.push(ready);
    for (let i = 0; i < 5; i++) {
      await vi.advanceTimersByTimeAsync(25_000);
      s.conns[0]?.push(': hb\n\n');
    }
    expect(s.conns[0]?.signal.aborted).toBe(false);
    s.stream.stop();
  });

  it('stop aborts the request and prevents reconnects', async () => {
    const s = setup();
    await flush();
    s.stream.stop();
    expect(s.conns[0]?.signal.aborted).toBe(true);
    await vi.advanceTimersByTimeAsync(120_000);
    expect(s.fetchMock).toHaveBeenCalledTimes(1);
    expect(s.statuses.at(-1)).toBe('stopped');
  });
});
