// The one HTTP path of @todayz/api-client (W0-04; 04 §1, §1.4). Every generated
// operation in src/gen calls request(); apps call configure() once at start-up.
//
//   configure({ baseUrl: 'https://api.todayz.in', token: () => auth.accessToken })
//   const page = await listMyOrders({ limit: 20 });   // src/gen/orders/orders.ts
//
// A non-2xx answer is thrown as ApiError carrying the problem+json body (code,
// status, fields, extensions, requestId), so a caller switches on err.code and
// translates it with the problems.<code> i18n key. A body that is not problem
// JSON (a proxy page, an empty 502) becomes ApiError with a registered code:
// "internal" for a 5xx and "bad_request" otherwise, so callers always see one
// shape (D-100 L7).

/** RFC 9457 problem details as the API sends them (schemas/common.yaml#/Problem). */
export interface Problem {
  type: string;
  title: string;
  status: number;
  code: string;
  detail?: string;
  requestId?: string;
  fields?: Record<string, string>;
  extensions?: Record<string, unknown>;
}

export interface ApiClientConfig {
  /** Origin of the API, no trailing slash (`https://api.todayz.in`). Default: same origin (''). */
  baseUrl?: string;
  /** Returns the access token for `Authorization: Bearer`, or null/undefined when signed out. */
  token?: () => string | null | undefined | Promise<string | null | undefined>;
  /** `Accept-Language` for translated category names and notification text. */
  language?: () => string | null | undefined;
  /** The fetch to use (tests inject one; defaults to globalThis.fetch at call time). */
  fetch?: typeof globalThis.fetch;
}

let config: ApiClientConfig = {};

/** Sets the client up for this app; call once at start-up, again to change the token source. */
export function configure(next: ApiClientConfig): void {
  config = { ...next };
}

/** What ApiError serialises to: never `detail`, `fields` or `extensions` (D-100, security L-6). */
export interface ApiErrorJSON {
  code: string;
  status: number;
  requestId?: string;
}

/** Thrown by request() for every non-2xx answer. */
export class ApiError extends Error {
  readonly status: number;
  readonly problem: Problem;
  readonly headers: Headers;

  constructor(problem: Problem, headers: Headers) {
    super(`${problem.code} (${problem.status}): ${problem.detail ?? problem.title}`);
    this.name = 'ApiError';
    this.status = problem.status;
    this.problem = problem;
    this.headers = headers;
  }

  /** The registered problem code (`insufficient_stock`), translated by `problems.<code>`. */
  get code(): string {
    return this.problem.code;
  }

  /** Field path → reason for `validation_failed` (`body.items[2].quantity`). */
  get fields(): Record<string, string> {
    return this.problem.fields ?? {};
  }

  /** Seconds to wait when the server sent `Retry-After`, else undefined. */
  get retryAfterSeconds(): number | undefined {
    const v = this.headers.get('retry-after');
    if (v === null) return undefined;
    const n = Number(v);
    return Number.isFinite(n) && n >= 0 ? n : undefined;
  }

  /**
   * Only the code, status and request id: an error that reaches a log, a crash
   * report or `POST /v1/client-errors` carries nothing a problem's detail or
   * fields could echo from the request.
   */
  toJSON(): ApiErrorJSON {
    const out: ApiErrorJSON = { code: this.code, status: this.status };
    if (this.problem.requestId) out.requestId = this.problem.requestId;
    return out;
  }
}

/** Options every generated call accepts after its parameters (plain RequestInit plus the key). */
export interface RequestOptions extends RequestInit {
  /** A client-made ULID for `Idempotency-Key` (04 §1.6); required where the operation says so. */
  idempotencyKey?: string;
  /** The `ETag` last read, sent as `If-Match` (412 on a mismatch). */
  ifMatch?: string;
}

const PROBLEM_TYPE = 'https://todayz.in/errors/';

/**
 * The sign-in routes authenticate by the refresh cookie or by nothing (04 §2.1): the bearer is
 * never sent to them, so a stale access token cannot turn a refresh into a 401 (W1-08).
 */
const NO_BEARER = new Set([
  '/v1/auth/otp/send',
  '/v1/auth/otp/verify',
  '/v1/auth/refresh',
  '/v1/auth/logout',
  '/v1/auth/logout-all',
]);

/**
 * The refresh cookie is set and read by /v1/auth/*, and the API is on another origin than the app:
 * without `credentials: 'include'` the browser drops the cookie and the next reload signs the person
 * out. Every /v1/auth/ call sends it (a caller can still say otherwise).
 */
const AUTH_PREFIX = '/v1/auth/';

function isProblem(v: unknown): v is Problem {
  if (typeof v !== 'object' || v === null) return false;
  const p = v as Record<string, unknown>;
  return typeof p['code'] === 'string' && typeof p['status'] === 'number' && typeof p['title'] === 'string';
}

/** A Problem for an answer that did not carry problem JSON; the code is a registered one. */
export function fallbackProblem(status: number, requestId?: string | null): Problem {
  const code = status >= 500 ? 'internal' : 'bad_request';
  const p: Problem = {
    type: PROBLEM_TYPE + code,
    title: status >= 500 ? 'The service is not available' : 'The request could not be read',
    status,
    code,
  };
  if (requestId) p.requestId = requestId;
  return p;
}

/** The paths request() sends: the API's namespaces and the health paths, nothing else (D-100 M-3). */
const ALLOWED_PATH = /^(\/v1\/|\/healthz$|\/readyz$|\/version$)/;

/**
 * Refuses a path a generated builder could not have produced from a well-formed
 * id: a dot segment (`/../`, `/./`, encoded as `%2e` or not), or a path outside
 * `/v1/` and the health paths. The server's path guard refuses the same shapes,
 * so this only keeps a bad id from leaving the app at all. Throws a TypeError:
 * it is a programming error, not an API answer.
 */
export function checkPath(url: string): void {
  // A backslash or a control character never comes from a builder; a proxy or
  // another server could read it as a separator (D-100 round 2, Low 11).
  if (/[\\\t\r\n]/.test(url)) {
    throw new TypeError('api-client: request() refuses a path with a backslash or a control character');
  }
  const path = url.split('?', 1)[0] ?? '';
  if (!ALLOWED_PATH.test(path)) {
    throw new TypeError('api-client: request() only sends paths under /v1/ or a health path');
  }
  const segments = (path + '/').split('/');
  if (segments.includes('.') || segments.includes('..') || /%2e/i.test(path)) {
    throw new TypeError('api-client: request() refuses a path with a dot segment');
  }
}

/**
 * The orval mutator: `request<T>(url, init)`. url is the path the generated code built
 * (`/v1/me/orders?limit=20`); init carries method, headers and the JSON body.
 * Resolves by the response's Content-Type: JSON (`application/json` or `+json`) is
 * decoded (undefined for 204/205 and an empty body), `text/*` (a CSV export) is the
 * text, anything else is a Blob. Rejects with ApiError.
 */
export async function request<T>(url: string, init: RequestOptions = {}): Promise<T> {
  checkPath(url);
  const { idempotencyKey, ifMatch, ...rest } = init;
  const headers = new Headers(rest.headers);
  // A caller that asked for something else (a CSV export, an SSE stream) keeps its Accept.
  if (!headers.has('Accept')) headers.set('Accept', 'application/json, application/problem+json');
  if (idempotencyKey) headers.set('Idempotency-Key', idempotencyKey);
  if (ifMatch) headers.set('If-Match', ifMatch);
  const token = NO_BEARER.has(url.split('?', 1)[0] ?? '') ? undefined : await config.token?.();
  if (token) headers.set('Authorization', `Bearer ${token}`);
  const lang = config.language?.();
  if (lang) headers.set('Accept-Language', lang);

  const doFetch = config.fetch ?? globalThis.fetch;
  const credentials = rest.credentials ?? (url.startsWith(AUTH_PREFIX) ? 'include' : undefined);
  const res = await doFetch((config.baseUrl ?? '') + url, {
    ...rest,
    ...(credentials ? { credentials } : {}),
    headers,
  });
  const requestId = res.headers.get('x-request-id');

  if (res.ok) {
    if (res.status === 204 || res.status === 205) return undefined as T;
    const type = (res.headers.get('content-type') ?? '').split(';', 1)[0]?.trim().toLowerCase() ?? '';
    if (type === 'application/json' || type.endsWith('+json') || type === '') {
      const text = await res.text();
      if (text === '') return undefined as T;
      return JSON.parse(text) as T;
    }
    if (type.startsWith('text/')) return (await res.text()) as T;
    return (await res.blob()) as T;
  }

  let body: unknown;
  try {
    body = await res.json();
  } catch {
    body = undefined;
  }
  const problem = isProblem(body) ? body : fallbackProblem(res.status, requestId);
  if (requestId && !problem.requestId) problem.requestId = requestId;
  throw new ApiError(problem, res.headers);
}

/** The type the generated code expects a mutator to return. */
export type RequestFn = typeof request;
