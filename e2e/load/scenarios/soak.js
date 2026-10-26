// soak (08 §6): browse and orders at 30% of peak (90 browsing VUs, about 15 orders a minute) for
// LOAD_SOAK_DURATION (08 says 2 h; make load runs 20 min by default and the report says so).
// The memory and connection verdict comes from sample.sh, which make load-soak runs alongside
// (API and worker RSS, Go heap and goroutines, pool and Postgres connections every 30 s).
//
// Each order VU is one shop person of pool C (4 stores) and one customer, both signed in by the
// VU itself and kept fresh with POST /v1/auth/refresh, so a run can outlast the 15 min token.
import { sleep } from 'k6';
import exec from 'k6/execution';
import { Counter } from 'k6/metrics';
import { visit } from './browse_peak.js';
import { fixture, get, post, metrics, rec, summary, signIn, keepFresh, poolNational, randomNational, STATS, SCALE, SOAK_POOL_SIZE, SOAK_STORES_EACH } from '../lib/common.js';

const F = fixture();
const DURATION = __ENV.LOAD_SOAK_DURATION || '20m';
const BROWSE_VUS = Math.max(1, Math.round(90 * SCALE));

const M = metrics({
  summary: 'read',
  stores: 'read',
  stores_category: 'read',
  categories: 'read',
  store: 'read',
  store_items: 'read',
  store_items_q: 'read',
  substitutes: 'read',
  search: 'search',
  suggest: 'search',
  quote: 'write',
  place: 'write',
  accept: 'write',
  ready: 'write',
  verify_code: 'write',
  order_get: 'read',
});
const collected = new Counter('n_orders_collected');

export const options = {
  summaryTrendStats: STATS,
  scenarios: {
    browse: { executor: 'constant-vus', vus: BROWSE_VUS, duration: DURATION, exec: 'browse' },
    orders: { executor: 'constant-vus', vus: SOAK_POOL_SIZE, duration: DURATION, exec: 'orders', gracefulStop: '2m' },
  },
  thresholds: {
    lat_read: ['p(95)<200'],
    lat_search: ['p(95)<300'],
    lat_write: ['p(95)<400'],
    err_all: ['rate<0.001'],
  },
};

export function browse() {
  visit(M);
}

const pick = (a) => a[Math.floor(Math.random() * a.length)];
let shop = null; // this VU's sessions
let cust = null;

export function orders() {
  const me = ((exec.vu.idInTest - 1) % SOAK_POOL_SIZE) + 1;
  if (!shop) {
    sleep(Math.random() * 20); // spread the 25 first sign-ins
    shop = signIn(poolNational('soak', me), 'business');
    cust = signIn(randomNational(), 'customer');
    if (!shop || !cust) {
      shop = null;
      sleep(30);
      return;
    }
  }
  if (!keepFresh(shop) || !keepFresh(cust)) {
    sleep(10);
    return;
  }
  const n = (me - 1) * SOAK_STORES_EACH + 1 + Math.floor(Math.random() * SOAK_STORES_EACH);
  const s = F.stores[n - 1];
  const base = `/v1/biz/businesses/${s.bid}/stores/${s.id}`;
  const lines = [];
  const used = new Set();
  for (let i = 0; i < 1 + Math.floor(Math.random() * 3); i++) {
    const it = pick(s.items);
    if (!used.has(it.id)) {
      used.add(it.id);
      lines.push({ storeItemId: it.id, quantity: 1 });
    }
  }
  const q = post('/v1/me/checkout/quote', { storeId: s.id, fulfilment: 'pickup', items: lines, creditsToUse: 0 }, cust.token, 'quote');
  if (!rec(M, 'quote', q) || !q.json('orderable')) return;
  const o = post('/v1/me/orders', { storeId: s.id, fulfilment: 'pickup', items: lines, creditsToUse: 0, expectedTotalPaise: q.json('totalPaise') }, cust.token, 'place');
  if (!rec(M, 'place', o, [200, 201])) return;
  const id = o.json('id');
  sleep(5 + Math.random() * 20);
  keepFresh(shop);
  if (!rec(M, 'accept', post(`${base}/orders/${id}/accept`, {}, shop.token, 'accept'))) return;
  sleep(10 + Math.random() * 30);
  keepFresh(shop);
  if (!rec(M, 'ready', post(`${base}/orders/${id}/ready`, {}, shop.token, 'ready'))) return;
  sleep(5 + Math.random() * 25);
  keepFresh(cust);
  const mine = get(`/v1/me/orders/${id}`, cust.token, 'order_get');
  if (!rec(M, 'order_get', mine)) return;
  keepFresh(shop);
  const v = post(`${base}/orders/${id}/verify-code`, { code: mine.json('code'), paymentMethod: 'cash' }, shop.token, 'verify_code');
  if (rec(M, 'verify_code', v) && v.json('status') === 'collected') collected.add(1);
  sleep(30 + Math.random() * 30); // 25 VUs at ~100 s an order: about 15 orders a minute
}

export function handleSummary(data) {
  return summary('soak', data, { duration: DURATION });
}
