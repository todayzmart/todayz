// order_burst (08 §6): 50 pickup orders a minute across 100 shops; the shops accept, mark ready
// and take the pickup code at the counter. About 10% of the orders placed in the first 3 minutes
// are left unaccepted so the accept timer (order_accept_timeout_min) fires inside the run; the
// run lasts until they have expired, and checks/order_burst.sql then measures how late each
// timer fired and runs the ledger reconcile.
// Pass: p95 writes < 400 ms; timers late < 10 s p99 (SQL); ledger reconcile clean (SQL).
import { sleep } from 'k6';
import exec from 'k6/execution';
import { Counter } from 'k6/metrics';
import { fixture, get, post, metrics, rec, summary, signIn, signInPool, randomNational, ownerOf, STATS, SCALE } from '../lib/common.js';

const F = fixture();
const STORES = 100; // load stores 1…100: owners 1…4 of pool A
const RATE = Math.max(1, Math.round(50 * SCALE));
const DURATION = __ENV.LOAD_ORDER_DURATION || '11m';
const CUSTOMERS = Number(__ENV.LOAD_CUSTOMERS || '80');
const LEAVE_WINDOW_MS = 3 * 60 * 1000;

const M = metrics({
  quote: 'write',
  place: 'write',
  accept: 'write',
  ready: 'write',
  verify_code: 'write',
  order_get: 'read',
  shop_orders: 'read',
});
const placed = new Counter('n_orders_placed');
const left = new Counter('n_orders_left_to_expire');
const collected = new Counter('n_orders_collected');

export const options = {
  summaryTrendStats: STATS,
  setupTimeout: '10m',
  scenarios: {
    orders: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1m',
      duration: DURATION,
      preAllocatedVUs: 60,
      maxVUs: 200,
      gracefulStop: '2m', // access tokens live 15 min: the run ends inside that
      exec: 'order',
    },
    shops: {
      executor: 'constant-vus',
      vus: 8,
      duration: DURATION,
      exec: 'shopPoll',
    },
    // Keeps the run open until the orders left unaccepted in the first 3 minutes have expired
    // (10 min accept timer + slack), so the SQL check sees every timer.
    tail: { executor: 'constant-vus', vus: 1, duration: '14m30s', exec: 'idle' },
  },
  thresholds: {
    lat_write: ['p(95)<400'],
    err_all: ['rate<0.001'],
  },
};

export function setup() {
  const owners = signInPool('owner', ownerOf(STORES));
  const customers = [];
  for (let i = 0; i < CUSTOMERS; i++) {
    const s = signIn(randomNational(), 'customer');
    if (s) customers.push(s.token);
  }
  if (customers.length < CUSTOMERS * 0.95) throw new Error(`only ${customers.length} of ${CUSTOMERS} customers signed in`);
  return { owners, customers, start: Date.now() };
}

const pick = (a) => a[Math.floor(Math.random() * a.length)];
const shopBase = (s) => `/v1/biz/businesses/${s.bid}/stores/${s.id}`;

export function order(data) {
  const n = 1 + Math.floor(Math.random() * STORES);
  const s = F.stores[n - 1];
  const shop = data.owners[ownerOf(n)];
  const cust = data.customers[exec.scenario.iterationInTest % data.customers.length];
  const lines = [];
  const used = new Set();
  const want = 1 + Math.floor(Math.random() * 3);
  for (let i = 0; i < want; i++) {
    const it = pick(s.items);
    if (used.has(it.id)) continue;
    used.add(it.id);
    lines.push({ storeItemId: it.id, quantity: 1 + Math.floor(Math.random() * 2) });
  }

  const q = post('/v1/me/checkout/quote', { storeId: s.id, fulfilment: 'pickup', items: lines, creditsToUse: 0 }, cust, 'quote');
  if (!rec(M, 'quote', q) || !q.json('orderable')) return;
  const o = post('/v1/me/orders', { storeId: s.id, fulfilment: 'pickup', items: lines, creditsToUse: 0, expectedTotalPaise: q.json('totalPaise') }, cust, 'place');
  if (!rec(M, 'place', o, [200, 201])) return;
  placed.add(1);
  const id = o.json('id');

  if (Date.now() - data.start < LEAVE_WINDOW_MS && Math.random() < 0.1) {
    left.add(1); // the accept timer expires it
    return;
  }
  sleep(5 + Math.random() * 20); // the shop hears it and opens it
  if (!rec(M, 'accept', post(`${shopBase(s)}/orders/${id}/accept`, {}, shop, 'accept'))) return;
  sleep(10 + Math.random() * 30); // packing
  if (!rec(M, 'ready', post(`${shopBase(s)}/orders/${id}/ready`, {}, shop, 'ready'))) return;
  sleep(5 + Math.random() * 25); // the customer walks over
  const mine = get(`/v1/me/orders/${id}`, cust, 'order_get');
  if (!rec(M, 'order_get', mine)) return;
  const code = mine.json('code'); // never printed
  const v = post(`${shopBase(s)}/orders/${id}/verify-code`, { code, paymentMethod: 'cash' }, shop, 'verify_code');
  if (rec(M, 'verify_code', v) && v.json('status') === 'collected') collected.add(1);
}

/** A shop screen refreshing its new-orders list, as the business app does on an SSE nudge. */
export function shopPoll(data) {
  const n = 1 + Math.floor(Math.random() * STORES);
  const s = F.stores[n - 1];
  rec(M, 'shop_orders', get(`${shopBase(s)}/orders`, data.owners[ownerOf(n)], 'shop_orders', { status: pick(['new', 'accepted', 'ready']), limit: 20 }));
  sleep(4 + Math.random() * 4);
}

export function idle() {
  sleep(870);
}

export function handleSummary(data) {
  return summary('order_burst', data);
}
