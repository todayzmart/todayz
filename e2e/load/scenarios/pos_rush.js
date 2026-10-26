// pos_rush (08 §6): 200 shops ringing one sale every 20 s each (10 sales/s), 90% online through
// POST …/pos/sales and 10% arriving offline through POST …/sync in batches of 20. Retries are
// part of the load: 2% of online sales are sent again with the same clientId (a lost answer) and
// 10% of sync batches are replayed whole; both must come back as the first sale or `duplicate`.
// checks/pos_rush.sql then confirms no (store, clientId) has two sales.
// Pass: p95 < 400 ms, no duplicate sales.
import { sleep } from 'k6';
import { Counter, Rate } from 'k6/metrics';
import { fixture, post, metrics, rec, summary, signInPool, ownerOf, ulid, STATS, SCALE } from '../lib/common.js';

const F = fixture();
const STORES = Math.round(200 * Math.min(1, SCALE)); // load stores 1…200: managers 1…8 of pool B
const DURATION = __ENV.LOAD_POS_DURATION || '10m';
const SALES_PER_MIN = Math.round(STORES * 3); // one sale per shop per 20 s
const ONLINE_PER_MIN = Math.round(SALES_PER_MIN * 0.9);
const BATCH = 20;
const SYNC_PER_MIN = Math.max(1, Math.round((SALES_PER_MIN * 0.1) / BATCH));

const M = metrics({ sale: 'write', sale_retry: 'write', sync: 'write', sync_replay: 'write' });
const sent = new Counter('n_sales_unique_sent'); // one per distinct clientId
const dupOk = new Rate('ok_retry_returned_same_sale');

export const options = {
  summaryTrendStats: STATS,
  setupTimeout: '5m',
  scenarios: {
    online: {
      executor: 'constant-arrival-rate',
      rate: ONLINE_PER_MIN,
      timeUnit: '1m',
      duration: DURATION,
      preAllocatedVUs: 30,
      maxVUs: 150,
      exec: 'online',
    },
    offline: {
      executor: 'constant-arrival-rate',
      rate: SYNC_PER_MIN,
      timeUnit: '1m',
      duration: DURATION,
      preAllocatedVUs: 2,
      maxVUs: 6,
      exec: 'offline',
    },
  },
  thresholds: {
    lat_write: ['p(95)<400'],
    err_all: ['rate<0.001'],
    ok_retry_returned_same_sale: ['rate==1'],
  },
};

export function setup() {
  return { managers: signInPool('manager', ownerOf(STORES)), start: new Date().toISOString() };
}

const pick = (a) => a[Math.floor(Math.random() * a.length)];
const base = (s) => `/v1/biz/businesses/${s.bid}/stores/${s.id}`;

function basket(s) {
  const lines = [];
  const used = new Set();
  const want = 1 + Math.floor(Math.random() * 5);
  for (let i = 0; i < want; i++) {
    const it = pick(s.items);
    if (used.has(it.id)) continue;
    used.add(it.id);
    lines.push({ clientId: ulid(), storeItemId: it.id, name: it.n, unitPricePaise: it.p, quantity: 1 + Math.floor(Math.random() * 3) });
  }
  return lines;
}

export function online(data) {
  const n = 1 + Math.floor(Math.random() * STORES);
  const s = F.stores[n - 1];
  const tok = data.managers[ownerOf(n)];
  const body = {
    clientId: ulid(),
    lines: basket(s).map(({ clientId, storeItemId, unitPricePaise, quantity }) => ({ clientId, storeItemId, unitPricePaise, quantity })),
    paymentMethod: pick(['cash', 'cash', 'upi', 'card']),
  };
  const r = post(`${base(s)}/pos/sales`, body, tok, 'sale');
  if (!rec(M, 'sale', r, [200, 201])) return;
  sent.add(1);
  if (Math.random() < 0.02) {
    // The answer was lost: the till sends the same sale again with a new key.
    const again = post(`${base(s)}/pos/sales`, body, tok, 'sale_retry');
    if (rec(M, 'sale_retry', again, [200, 201])) dupOk.add(again.json('id') === r.json('id'));
  }
}

// The catalogue cursor each till last synced to, per store (this VU's tills). A till that has
// never synced starts at 0, which the generated stores allow (their first change is seq 1).
const cursors = {};

export function offline(data) {
  const n = 1 + Math.floor(Math.random() * STORES);
  const s = F.stores[n - 1];
  const tok = data.managers[ownerOf(n)];
  const now = Date.now();
  const seq0 = now * 100; // strictly increasing within the batch
  const entries = [];
  for (let i = 0; i < BATCH; i++) {
    entries.push({
      clientId: ulid(now - (BATCH - i) * 20000),
      deviceSeq: seq0 + i + 1,
      kind: 'sale',
      at: new Date(now - (BATCH - i) * 20000).toISOString(), // rung offline over the last ~7 min
      sale: { lines: basket(s), paymentMethod: 'cash' },
    });
  }
  const cursor = cursors[s.id] || 0;
  const r = post(`${base(s)}/sync`, { cursor, entries }, tok, 'sync');
  if (!rec(M, 'sync', r)) return;
  cursors[s.id] = r.json('cursor');
  const results = r.json('results') || [];
  const applied = results.filter((x) => x.status === 'applied').length;
  sent.add(applied);
  if (applied !== BATCH) console.warn(`sync applied ${applied} of ${BATCH} (statuses: ${[...new Set(results.map((x) => x.status))].join(',')})`);
  if (Math.random() < 0.1) {
    sleep(1);
    const again = post(`${base(s)}/sync`, { cursor: cursors[s.id], entries }, tok, 'sync_replay');
    if (rec(M, 'sync_replay', again)) {
      const res = again.json('results') || [];
      dupOk.add(res.length === BATCH && res.every((x) => x.status === 'duplicate'));
    }
  }
}

export function handleSummary(data) {
  return summary('pos_rush', data, { stores: STORES, onlinePerMin: ONLINE_PER_MIN, syncPerMin: SYNC_PER_MIN, batch: BATCH });
}
