// browse_peak (08 §6): 300 VUs browsing anonymously. Each iteration is one visit: home summary,
// shop list, a search with two suggest keystrokes before it, a shop page, its items, and a
// product's substitutes, with 3–8 s of think time between screens.
// Pass: p95 reads < 200 ms, p95 search < 300 ms, errors < 0.1%.
import { sleep, group } from 'k6';
import { fixture, get, metrics, rec, summary, STATS, SCALE } from '../lib/common.js';

const F = fixture();
const VUS = Math.max(1, Math.round(300 * SCALE));
const HOLD = __ENV.LOAD_HOLD || '8m';

export const M = metrics({
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
});

export const options = {
  summaryTrendStats: STATS,
  scenarios: {
    browse: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '2m', target: VUS },
        { duration: HOLD, target: VUS },
        { duration: '30s', target: 0 },
      ],
      gracefulRampDown: '15s',
    },
  },
  thresholds: {
    lat_read: ['p(95)<200'],
    lat_search: ['p(95)<300'],
    err_all: ['rate<0.001'],
  },
};

const pick = (a) => a[Math.floor(Math.random() * a.length)];
const think = () => sleep(3 + Math.random() * 5);
const jitter = (v) => v + (Math.random() - 0.5) * 0.04; // about ±2 km

/** One visit. Exported so soak.js reuses it. */
export function visit(m = M) {
  const city = pick(F.cities);
  const lat = jitter(city.lat);
  const lng = jitter(city.lng);
  const radius = pick([2000, 2000, 2000, 5000, 1000]);

  group('home', () => {
    rec(m, 'summary', get('/v1/discover/summary', null, 'summary', { lat, lng, radius }));
    if (Math.random() < 0.3) rec(m, 'categories', get('/v1/discover/categories', null, 'categories'));
    const list = get('/v1/discover/stores', null, 'stores', { lat, lng, radius, limit: 20 });
    rec(m, 'stores', list);
  });
  think();

  if (Math.random() < 0.4) {
    rec(m, 'stores_category', get('/v1/discover/stores', null, 'stores_category', { lat, lng, radius, category: pick(F.categories), openNow: true, limit: 20 }));
    think();
  }

  const term = pick(F.terms);
  group('search', () => {
    rec(m, 'suggest', get('/v1/discover/search/suggest', null, 'suggest', { q: term.slice(0, 2), limit: 8 }));
    rec(m, 'suggest', get('/v1/discover/search/suggest', null, 'suggest', { q: term.slice(0, 4), limit: 8 }));
    rec(m, 'search', get('/v1/discover/search', null, 'search', { q: term, lat, lng, radius }));
  });
  think();

  // A shop in this city (the search result's shop in the real app; a fixture shop keeps the
  // load independent of ranking).
  const shops = F.byCity[city.name];
  const s = F.stores[pick(shops)];
  group('shop', () => {
    rec(m, 'store', get(`/v1/discover/stores/${s.slug}`, null, 'store', { lat, lng }));
    rec(m, 'store_items', get(`/v1/discover/stores/${s.slug}/items`, null, 'store_items', { limit: 30 }));
  });
  think();

  if (Math.random() < 0.5) {
    rec(m, 'store_items_q', get(`/v1/discover/stores/${s.slug}/items`, null, 'store_items_q', { q: pick(F.terms), limit: 30 }));
    think();
  }

  const item = pick(s.items);
  rec(m, 'substitutes', get(`/v1/discover/stores/${s.slug}/substitutes`, null, 'substitutes', { itemId: item.id, lat, lng }));
  think();
}

export default function () {
  visit();
}

export function handleSummary(data) {
  return summary('browse_peak', data);
}
