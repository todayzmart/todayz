import { createHash, createHmac, randomBytes } from 'node:crypto';

import { expect, type APIRequestContext, type APIResponse } from '@playwright/test';

import { URLS } from './seed';

const CROCKFORD = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

/** A ULID (Idempotency-Key, client ids and device ids must be ULIDs). */
export function ulid(at = Date.now()): string {
  let time = '';
  let t = at;
  for (let i = 0; i < 10; i++) {
    time = (CROCKFORD[t % 32] ?? '0') + time;
    t = Math.floor(t / 32);
  }
  const rand = randomBytes(16);
  let tail = '';
  for (let i = 0; i < 16; i++) tail += CROCKFORD[(rand[i] ?? 0) % 32];
  return time + tail;
}

/** RFC 6238 over a base32 secret: SHA-1, `digits` digits, `period` seconds (the POS counter code, D-12). */
export function totp(secret: string, at = Date.now(), digits = 6, period = 30): string {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  let bits = '';
  for (const ch of secret.replace(/=+$/, '').toUpperCase()) bits += alphabet.indexOf(ch).toString(2).padStart(5, '0');
  const key = Buffer.from(bits.match(/.{8}/g)?.map((b) => parseInt(b, 2)) ?? []);
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(at / 1000 / period)));
  const mac = createHmac('sha1', key).update(counter).digest();
  const offset = (mac[mac.length - 1] ?? 0) & 0x0f;
  const n = (mac.readUInt32BE(offset) & 0x7fffffff) % 10 ** digits;
  return String(n).padStart(digits, '0');
}

/** A short, non-secret fingerprint for messages (never the value itself). */
export function fingerprint(value: string): string {
  return createHash('sha256').update(value).digest('hex').slice(0, 8);
}

export interface Reply<T = unknown> {
  status: number;
  body: T;
  etag: string;
  /** The problem code of an error answer, else ''. */
  code: string;
}

export interface CallOptions {
  /** Send an Idempotency-Key (a fresh ULID, or this one). */
  idem?: boolean | string;
  ifMatch?: string;
  headers?: Record<string, string>;
  params?: Record<string, string | number | boolean>;
  /** A multipart form (an upload): sent instead of `data`. */
  multipart?: Record<string, string | number | boolean | { name: string; mimeType: string; buffer: Buffer }>;
}

async function reply<T>(r: APIResponse): Promise<Reply<T>> {
  const text = await r.text();
  let body: unknown = null;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    body = null;
  }
  const code = r.status() >= 400 && body && typeof body === 'object' && 'code' in body ? String((body as { code: unknown }).code) : '';
  return { status: r.status(), body: body as T, etag: r.headers()['etag'] ?? '', code };
}

/** What an Api needs from a signed-in person (principals.ts). */
export interface Bearer {
  /** The request context holding this person's refresh cookie (a browser context's while lent). */
  request(): APIRequestContext;
  token(): Promise<string>;
  /** Gets a new access token with the refresh cookie (after a 401). */
  renew(): Promise<void>;
}

/**
 * A JSON client for one signed-in person. Failure messages carry the method, path, status and problem
 * code only: bodies can hold pickup codes, bill tokens or phones, which never go into test output.
 * A plain 401 `unauthorized` (the session rotated in a browser, or the test clock moved past the
 * token's life) renews once and retries with the same Idempotency-Key.
 */
export class Api {
  constructor(private readonly who: Bearer) {}

  async call<T = unknown>(method: 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE', path: string, data?: unknown, opts: CallOptions = {}): Promise<Reply<T>> {
    const idem = opts.idem ? (typeof opts.idem === 'string' ? opts.idem : ulid()) : undefined;
    const send = async () => {
      const headers: Record<string, string> = { Authorization: `Bearer ${await this.who.token()}`, ...opts.headers };
      if (idem) headers['Idempotency-Key'] = idem;
      if (opts.ifMatch) headers['If-Match'] = opts.ifMatch;
      const r = await this.who.request().fetch(`${URLS.api}${path}`, {
        method,
        headers,
        ...(opts.multipart ? { multipart: opts.multipart } : data === undefined ? {} : { data }),
        ...(opts.params ? { params: opts.params } : {}),
      });
      return reply<T>(r);
    };
    let r = await send();
    if (r.status === 401 && (r.code === 'unauthorized' || r.code === '')) {
      await this.who.renew();
      r = await send();
    }
    return r;
  }

  get<T = unknown>(path: string, params?: Record<string, string | number | boolean>) {
    return this.call<T>('GET', path, undefined, params ? { params } : {});
  }

  post<T = unknown>(path: string, data: unknown = {}, opts: CallOptions = {}) {
    return this.call<T>('POST', path, data, { idem: true, ...opts });
  }

  patch<T = unknown>(path: string, data: unknown, opts: CallOptions = {}) {
    return this.call<T>('PATCH', path, data, opts);
  }

  /** Calls and expects one of `want`; the message names the step and the problem code, never the body. */
  async ok<T = unknown>(step: string, method: 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE', path: string, data?: unknown, opts: CallOptions & { want?: number[] } = {}): Promise<T> {
    const want = opts.want ?? (method === 'POST' ? [200, 201] : [200]);
    const r = await this.call<T>(method, path, data, method === 'POST' ? { idem: true, ...opts } : opts);
    expect(want, `${step}: ${method} ${path} answered ${r.status}${r.code ? ` ${r.code}` : ''}`).toContain(r.status);
    return r.body;
  }
}

/** Unauthenticated JSON call through the same reply shape. */
export async function anon<T = unknown>(request: APIRequestContext, method: 'GET' | 'POST', path: string, data?: unknown, headers: Record<string, string> = {}): Promise<Reply<T>> {
  const r = await request.fetch(`${URLS.api}${path}`, { method, headers, ...(data === undefined ? {} : { data }) });
  return reply<T>(r);
}
