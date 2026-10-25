import type { Page, Request } from '@playwright/test';

/**
 * The smoke and area runs have no API: the page's calls are answered here. The API is on another
 * origin, so the cookie-carrying calls need the preflight and an explicit origin in the answer.
 */
const cors = (origin: string) => ({
  'access-control-allow-origin': origin,
  'access-control-allow-credentials': 'true',
  'access-control-allow-headers': 'authorization, content-type, x-tz-client, accept-language, accept, idempotency-key, if-match',
  'access-control-allow-methods': 'GET, POST, PATCH, PUT, DELETE, OPTIONS',
});

export const NOW = '2026-10-25T06:30:00Z';

export const problem = (status: number, code: string) => ({ type: `about:blank#${code}`, title: code, status, code });

/** What a faked call answers; `undefined` passes it to the next (earlier registered) handler. */
export type Answer = { status: number; body: unknown } | undefined;

/**
 * Answers calls whose path matches `path` (a glob after the origin, e.g. `/v1/admin/stores*`).
 * Playwright tries the newest route first, so register the catch-alls before the specific ones.
 */
export async function answerWith(page: Page, path: string, reply: (req: Request, url: URL) => Answer) {
  await page.route(`**${path}`, async (route) => {
    const req = route.request();
    const headers = cors(req.headers()['origin'] ?? '*');
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers });
    const r = reply(req, new URL(req.url()));
    if (!r) return route.fallback();
    return route.fulfill({
      status: r.status,
      contentType: r.status < 400 ? 'application/json' : 'application/problem+json',
      headers,
      body: JSON.stringify(r.body),
    });
  });
}

export function answer(page: Page, path: string, status: number, body: unknown) {
  return answerWith(page, path, () => ({ status, body }));
}

export function answerRefresh(page: Page, signedIn: boolean) {
  return signedIn
    ? answer(page, '/v1/auth/refresh', 200, {
        accessToken: 'smoke-token',
        expiresAt: new Date(Date.now() + 3_600_000).toISOString(),
        tokenType: 'Bearer',
        userId: '01JADMIN0000000000000000AD',
      })
    : answer(page, '/v1/auth/refresh', 401, problem(401, 'unauthorized'));
}

/** `GET /v1/me` for a staff member with these roles. */
export function answerMe(page: Page, roles: readonly string[]) {
  return answer(page, '/v1/me', 200, {
    id: '01JADMIN0000000000000000AD',
    name: 'Asha Rao',
    preferredLang: 'en',
    phoneMasked: '90•••••001',
    app: 'admin',
    roles,
    createdAt: '2026-10-01T04:30:00Z',
  });
}

/** Every other admin read answers an empty page; writes nobody faked are a 404. */
export async function answerEmptyLists(page: Page) {
  await answerWith(page, '/v1/admin/**', (req) =>
    req.method() === 'GET' ? { status: 200, body: { items: [], nextCursor: null, serverNow: NOW } } : { status: 404, body: problem(404, 'not_found') },
  );
  // A-17 lists every setting at once, with the provider keys beside them.
  await answerWith(page, '/v1/admin/settings', (req) => (req.method() === 'GET' ? { status: 200, body: { items: [], providers: [], serverNow: NOW } } : undefined));
  // A-09 shows the coming week's expiry as a total and its days (one summary, not a list).
  await answerWith(page, '/v1/admin/credits/expiry-preview**', (req) =>
    req.method() === 'GET' ? { status: 200, body: { from: NOW, to: NOW, totalPaise: 0, days: [], serverNow: NOW } } : undefined,
  );
  // A-02 Pulse is one summary, not a list: a platform with nothing in it yet.
  await answerWith(page, '/v1/admin/pulse**', (_req, url) => ({ status: 200, body: url.pathname.endsWith('/map') ? emptyPulseMap : emptyPulse }));
}

const count0 = { value: 0, previous: null };
const rate0 = { value: null, previous: null };

/** A-02 before any order, search or store. */
export const emptyPulse = {
  range: '7d',
  from: '2026-10-19',
  to: '2026-10-25',
  asOf: null,
  kpis: {
    gmvPaise: count0,
    onlineGmvPaise: count0,
    posGmvPaise: count0,
    orders: count0,
    completedOrders: count0,
    successRateBp: rate0,
    activeCustomers: count0,
    transactingCustomers: count0,
    liveStores: count0,
    activeStores: count0,
    newStores: count0,
    searches: count0,
    searchSuccessRateBp: rate0,
    d7RetentionBp: rate0,
  },
  trend: [],
  funnel: { searches: 0, searchesWithResults: 0, storeViews: 0, ordersPlaced: 0, ordersAccepted: 0, ordersCompleted: 0 },
  leaks: [],
  retention: { d1Bp: null, d7Bp: null, d30Bp: null, cohorts: [] },
  credits: { issued: 0, spent: 0, expired: 0 },
  zeroResultSearches: [],
  storesNeedingAttention: [],
  serverNow: NOW,
};

export const emptyPulseMap = { range: '7d', from: '2026-10-19', to: '2026-10-25', stores: [], demand: [], serverNow: NOW };

/**
 * Signed in as staff with these roles and past the authenticator step. The session and 2FA flags are
 * routing hints only; the API checks the token.
 */
export async function signIn(page: Page, roles: readonly string[]) {
  await answerEmptyLists(page);
  await answerRefresh(page, true);
  await answerMe(page, roles);
  await page.addInitScript(() => {
    localStorage.setItem('todayz.hasSession.admin', '1');
    sessionStorage.setItem('todayz.admin.2fa', String(Date.now()));
  });
}
