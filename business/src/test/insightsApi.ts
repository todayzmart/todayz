import type { Call, Handler } from './render';
import { json } from './render';
import { ALL_INSIGHTS, analytics, CUSTOMERS, INSIGHTS, overview, PERIOD, SERVER_NOW, STORE } from './insightsData';

export { ALL_INSIGHTS, analytics, CUSTOMERS, INSIGHTS, overview, SERVER_NOW, STORE };

export function shops(role = 'owner'): Handler {
  return (c: Call) =>
    c.path === '/v1/biz/businesses'
      ? json(200, {
          items: [
            { id: 'b1', status: 'verified', brandName: 'Sri Lakshmi', role, stores: [{ id: 's1', name: 'Sri Lakshmi Provision Stores', slug: 'sri-lakshmi', status: 'live' }] },
          ],
          nextCursor: null,
        })
      : undefined;
}

/** `GET {S}/insights` (the card, or `all=1`, optionally `kind=`), and `POST {S}/insights/dismiss`. */
export function insights(opts: { card?: unknown[]; all?: unknown[] } = {}): Handler {
  return (c) => {
    if (c.path === `${STORE}/insights` && c.method === 'GET') {
      const q = new URLSearchParams(c.search);
      let items = (q.get('all') === '1' ? (opts.all ?? ALL_INSIGHTS) : (opts.card ?? INSIGHTS)) as { kind: string }[];
      const kind = q.get('kind');
      if (kind) items = items.filter((i) => i.kind === kind);
      return json(200, { items, nextCursor: null, period: PERIOD, serverNow: SERVER_NOW });
    }
    if (c.path === `${STORE}/insights/dismiss` && c.method === 'POST') {
      return json(200, { quietUntil: '2026-02-04T18:30:00.000Z' });
    }
    return undefined;
  };
}

export function overviewHandler(over: Record<string, unknown> = {}): Handler {
  return (c) => (c.path === `${STORE}/overview` && c.method === 'GET' ? json(200, overview({ range: new URLSearchParams(c.search).get('range') ?? 'today', ...over })) : undefined);
}

/** Everything the Overview needs, signed in as `role`. */
export const ownerApi = (role = 'owner'): Handler[] => [shops(role), overviewHandler(), insights()];

export function analyticsHandler(over: Record<string, unknown> = {}): Handler {
  return (c) =>
    c.path === `${STORE}/analytics` && c.method === 'GET'
      ? json(200, { ...analytics(new URLSearchParams(c.search).get('range') ?? '7d'), ...over })
      : undefined;
}

/** `GET {S}/customers`: one page per segment, or `enough: false` when too few customers are linked. */
export function customersHandler(opts: { enough?: boolean; pages?: number } = {}): Handler {
  return (c) => {
    if (c.path !== `${STORE}/customers` || c.method !== 'GET') return undefined;
    const q = new URLSearchParams(c.search);
    const segment = q.get('segment') ?? 'all';
    const page = Number(q.get('cursor') ?? '0');
    const enough = opts.enough ?? true;
    const items = !enough ? [] : segment === 'slipping' ? CUSTOMERS.slice(1) : page > 0 ? [{ ...CUSTOMERS[0], customerId: 'c9', name: 'Ravi K.' }] : CUSTOMERS;
    const more = enough && page + 1 < (opts.pages ?? 1);
    return json(200, { segment, enough, total: items.length, windowDays: 90, lapseDays: 30, items, nextCursor: more ? String(page + 1) : null, serverNow: SERVER_NOW });
  };
}
