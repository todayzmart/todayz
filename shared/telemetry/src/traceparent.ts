// W3C trace context for API calls (05 §2.3, §2.5): a slow screen in RUM links to the backend trace.

const HEX = '0123456789abcdef';

function randomHex(bytes: number): string {
  const buf = new Uint8Array(bytes);
  if (typeof crypto !== 'undefined' && typeof crypto.getRandomValues === 'function') {
    crypto.getRandomValues(buf);
  } else {
    for (let i = 0; i < bytes; i++) buf[i] = Math.floor(Math.random() * 256);
  }
  // An all-zero id is invalid in W3C trace context.
  if (buf.every((b) => b === 0)) buf[bytes - 1] = 1;
  let out = '';
  for (const b of buf) out += HEX.charAt(b >> 4) + HEX.charAt(b & 15);
  return out;
}

/** `00-<32 hex trace id>-<16 hex span id>-01` (sampled). */
export function newTraceparent(): string {
  return `00-${randomHex(16)}-${randomHex(8)}-01`;
}

export const TRACEPARENT_PATTERN = /^00-[0-9a-f]{32}-[0-9a-f]{16}-01$/;

export interface TracedFetchOptions {
  /** Origins that get the header (`https://api.todayz.in`). A relative URL counts as the page's own origin. */
  apiOrigins: readonly string[];
  /** Called after each traced call settles, with the method, path (no query) and wall time. */
  onTiming?: ((t: { method: string; path: string; ms: number; status: number }) => void) | undefined;
  /** The page's origin, for relative URLs. Defaults to `location.origin`. */
  pageOrigin?: string | undefined;
}

/**
 * Wraps fetch so calls to our own API carry a fresh `traceparent`. Third-party hosts never get it, and a
 * caller that already set one keeps it.
 */
export function tracedFetch(base: typeof fetch, opts: TracedFetchOptions): typeof fetch {
  const page = opts.pageOrigin ?? (typeof location !== 'undefined' ? location.origin : '');
  const allowed = new Set(opts.apiOrigins.map((o) => o.replace(/\/$/, '')));
  return (async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    let url: URL | undefined;
    try {
      const raw = input instanceof Request ? input.url : String(input);
      url = new URL(raw, page || undefined);
    } catch {
      url = undefined;
    }
    if (!url || !allowed.has(url.origin)) return base(input, init);

    const headers = new Headers(init?.headers ?? (input instanceof Request ? input.headers : undefined));
    if (!headers.has('traceparent')) headers.set('traceparent', newTraceparent());
    const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
    const started = performance.now();
    let status = 0;
    try {
      const res = await base(input, { ...init, headers });
      status = res.status;
      return res;
    } finally {
      opts.onTiming?.({ method, path: url.pathname, ms: performance.now() - started, status });
    }
  }) as typeof fetch;
}
