// Shared helpers for the k6 scenarios (W4-02). Nothing here prints a phone, an OTP, a pickup code
// or a token: requests are logged by route name and status only (e2e/CLAUDE.md).
import http from 'k6/http';
import { check, fail, sleep } from 'k6';
import { SharedArray } from 'k6/data';
import { Trend, Rate, Counter } from 'k6/metrics';

export const API = __ENV.LOAD_API_URL || 'http://127.0.0.1:8180';
export const MOCKS = __ENV.LOAD_MOCKS_URL || 'http://127.0.0.1:4010';
export const OUT = __ENV.LOAD_OUT || 'out';
export const SCALE = Number(__ENV.LOAD_SCALE || '1');

// Pool A owns every load store, 25 stores per owner (owner n has stores 25(n-1)+1 … 25n); pool B
// is a manager of the same 25 stores. The SQL linker (data/link_staff.sql) finds them by name.
export const STORES_PER_OWNER = 25;
export const POOL_SIZE = 40;
// Pool C (soak) has 25 managers of 4 stores each (stores 1…100), one per soak VU, so each VU
// refreshes only its own session.
export const POOL = {
  owner: { prefix: '701', name: 'Load owner' },
  manager: { prefix: '702', name: 'Load manager' },
  soak: { prefix: '703', name: 'Load soak' },
};
export const SOAK_POOL_SIZE = 25;
export const SOAK_STORES_EACH = 4;

export const STATS = ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max', 'count'];

/** The launch-sized fixture exported after load-data (ids, slugs, a few priced items per store). */
export function fixture() {
  return new SharedArray('fixture', () => {
    const f = JSON.parse(open(__ENV.LOAD_FIXTURE));
    return [f];
  })[0];
}

const CROCKFORD = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

/** A ULID (Idempotency-Key, client ids, device ids). */
export function ulid(at = Date.now()) {
  let time = '';
  let t = at;
  for (let i = 0; i < 10; i++) {
    time = CROCKFORD[t % 32] + time;
    t = Math.floor(t / 32);
  }
  let tail = '';
  for (let i = 0; i < 16; i++) tail += CROCKFORD[Math.floor(Math.random() * 32)];
  return time + tail;
}

/** A Postgres uuid as the API's ULID text (130 bits, two leading zero bits). */
export function uuidToUlid(u) {
  const hex = u.replace(/-/g, '');
  let bits = '00';
  for (let i = 0; i < 32; i++) bits += parseInt(hex[i], 16).toString(2).padStart(4, '0');
  let out = '';
  for (let i = 0; i < 130; i += 5) out += CROCKFORD[parseInt(bits.slice(i, i + 5), 2)];
  return out;
}

/** A client IP from 100.64.0.0/10 (CGNAT), new per sign-in, sent as CF-Connecting-IP. */
export function fakeIp() {
  const r = () => Math.floor(Math.random() * 256);
  return `100.${64 + Math.floor(Math.random() * 64)}.${r()}.${r()}`;
}

/** A random 10-digit mobile number outside the reserved test range (+91 90000 000xx). */
export function randomNational() {
  let s = String(8);
  for (let i = 0; i < 9; i++) s += String(Math.floor(Math.random() * 10));
  return s;
}

export function poolNational(pool, n) {
  return POOL[pool].prefix + String(n).padStart(7, '0');
}

export function ownerOf(storeIndex) {
  return Math.floor((storeIndex - 1) / STORES_PER_OWNER) + 1;
}

// --- metrics -------------------------------------------------------------------------------

/**
 * Per-route latency trends and per-kind error rates. Call at init (k6 creates metrics only
 * there). names maps a route name to its kind: read, search or write.
 */
export function metrics(names) {
  const m = { route: {}, kind: {}, err: {}, errAll: new Rate('err_all'), unexpected: new Counter('unexpected_status') };
  for (const [name, kind] of Object.entries(names)) {
    m.route[name] = new Trend(`lat_${kind}_${name}`, true);
    if (!m.kind[kind]) {
      m.kind[kind] = new Trend(`lat_${kind}`, true);
      m.err[kind] = new Rate(`err_${kind}`);
    }
  }
  m.names = names;
  return m;
}

/**
 * Records one response: latency under its route and kind, and an error unless the status is
 * one of ok. Returns true when the status was expected.
 */
export function rec(m, name, res, ok = [200]) {
  const kind = m.names[name];
  if (!kind) fail(`metric route ${name} not registered`);
  const good = ok.includes(res.status);
  m.route[name].add(res.timings.duration);
  m.kind[kind].add(res.timings.duration);
  m.err[kind].add(!good);
  m.errAll.add(!good);
  if (!good) {
    m.unexpected.add(1, { name, status: String(res.status) });
    // Route, status and problem code only: never a body.
    let code = '';
    try {
      code = res.json('code') || '';
    } catch (_) {
      code = '';
    }
    console.warn(`${name} answered ${res.status} ${code}`);
  }
  return good;
}

// --- HTTP ------------------------------------------------------------------------------------

export function headers(token, extra = {}) {
  const h = { 'Content-Type': 'application/json', Accept: 'application/json', ...extra };
  if (token) h.Authorization = `Bearer ${token}`;
  return h;
}

export function get(path, token, name, params = {}) {
  const qs = Object.entries(params)
    .filter(([, v]) => v !== undefined && v !== null)
    .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`)
    .join('&');
  return http.get(`${API}${path}${qs ? `?${qs}` : ''}`, { headers: headers(token), tags: { name } });
}

/** A POST with a fresh Idempotency-Key unless extra names one (the apps send one on every POST). */
export function post(path, body, token, name, extra = {}) {
  const h = headers(token, { 'Idempotency-Key': ulid(), ...extra });
  return http.post(`${API}${path}`, JSON.stringify(body), { headers: h, tags: { name } });
}

// --- sign-in ---------------------------------------------------------------------------------

/**
 * One OTP sign-in through the mock SMS. Each sign-in comes from its own fake client IP, so the
 * per-IP caps (20 sends/hour, risk_otp_phones_per_ip_hour) never trip on a load run; the
 * per-phone cap (5/hour) is why the pools are split across scenarios. Returns null on failure.
 */
export function signIn(national, app, ip = fakeIp()) {
  const phone = `+91${national}`;
  const h = { 'Content-Type': 'application/json', 'CF-Connecting-IP': ip };
  let requestId = '';
  let since = '';
  for (let attempt = 0; attempt < 2; attempt++) {
    since = new Date(Date.now() - 1000).toISOString();
    const r = http.post(`${API}/v1/auth/otp/send`, JSON.stringify({ phone, purpose: 'login', app, turnstileToken: 'XXXX.DUMMY.TOKEN.XXXX' }), {
      headers: h,
      tags: { name: 'otp_send' },
    });
    if (r.status === 200) {
      requestId = r.json('requestId');
      break;
    }
    const wait = Number(r.headers['Retry-After'] || '0');
    if (r.status === 429 && wait > 0 && wait <= 35 && attempt === 0) {
      // The 30 s gap between two sends to one phone: wait once. Hourly caps are not waited for.
      sleep(wait + 1);
      continue;
    }
    console.warn(`sign-in (${app}): otp send answered ${r.status}`);
    return null;
  }
  if (!requestId) return null;
  const o = http.get(`${MOCKS}/_mock/sms/latest-otp?to=${encodeURIComponent(phone)}&since=${encodeURIComponent(since)}&wait=8`, { tags: { name: 'mock_otp' } });
  if (o.status !== 200) {
    console.warn(`sign-in (${app}): mocks answered ${o.status}`);
    return null;
  }
  const v = http.post(`${API}/v1/auth/otp/verify`, JSON.stringify({ requestId, code: o.json('otp'), app, deviceId: ulid() }), {
    headers: h,
    tags: { name: 'otp_verify' },
  });
  if (!check(v, { 'otp verify 200': (x) => x.status === 200 })) {
    console.warn(`sign-in (${app}): verify answered ${v.status}`);
    return null;
  }
  const c = v.cookies[COOKIE[app]];
  return { app, token: v.json('accessToken'), userId: v.json('userId'), expiresAt: Date.parse(v.json('expiresAt')), refresh: c && c.length ? c[0].value : '' };
}

const COOKIE = { customer: '__Secure-tz_rt_c', business: '__Secure-tz_rt_b' };
const ORIGIN = { customer: 'https://app.todayz.localhost', business: 'https://business.todayz.localhost' };

/**
 * Rotates sess's refresh cookie for a new access token when the access token has under 2 min
 * left (POST /v1/auth/refresh, as the app does). Only the VU that signed in may call this: the
 * token rotates on every use and a reused one revokes the whole session family. Returns false on
 * failure.
 */
export function keepFresh(sess) {
  if (!sess || sess.expiresAt - Date.now() > 120000) return true;
  const r = http.post(`${API}/v1/auth/refresh`, JSON.stringify({ app: sess.app }), {
    headers: {
      'Content-Type': 'application/json',
      Origin: ORIGIN[sess.app],
      'X-TZ-Client': `${sess.app}/9.9.9`,
      Cookie: `${COOKIE[sess.app]}=${sess.refresh}`,
    },
    tags: { name: 'refresh' },
  });
  if (r.status !== 200) {
    console.warn(`refresh (${sess.app}) answered ${r.status}`);
    return false;
  }
  const c = r.cookies[COOKIE[sess.app]];
  sess.token = r.json('accessToken');
  sess.expiresAt = Date.parse(r.json('expiresAt'));
  if (c && c.length) sess.refresh = c[0].value;
  return true;
}

/** Signs in pool members 1..n of pool (business app), keyed by member number. */
export function signInPool(pool, n) {
  const out = {};
  for (let i = 1; i <= n; i++) {
    const s = signIn(poolNational(pool, i), 'business');
    if (!s) fail(`pool ${pool} member ${i} could not sign in (make reset clears the hourly OTP caps)`);
    out[i] = s.token;
  }
  return out;
}

// --- summary ---------------------------------------------------------------------------------

/**
 * handleSummary: a compact table of every lat_* trend (p50, p95, p99, max, count), every err_*
 * rate and the thresholds' verdicts, to stdout and to <out>/<scenario>-summary.json. The JSON
 * holds metric values only (no request bodies, tokens or phones).
 */
export function summary(scenario, data, extra = {}) {
  const rows = [];
  const ms = (v) => (v === undefined ? '-' : `${v.toFixed(1)}`);
  const pick = {};
  for (const [name, m] of Object.entries(data.metrics)) {
    if (m.type === 'trend' && (name.startsWith('lat_') || name === 'http_req_duration' || name.startsWith('sse_') || name.startsWith('timer_'))) {
      const v = m.values;
      rows.push(`${name.padEnd(44)} n=${String(v.count ?? '').padStart(7)}  p50=${ms(v.med).padStart(8)}  p95=${ms(v['p(95)']).padStart(8)}  p99=${ms(v['p(99)']).padStart(8)}  max=${ms(v.max).padStart(8)}`);
      pick[name] = { count: v.count, p50: v.med, p95: v['p(95)'], p99: v['p(99)'], max: v.max };
    } else if (m.type === 'rate' && (name.startsWith('err_') || name === 'http_req_failed' || name.startsWith('ok_'))) {
      rows.push(`${name.padEnd(44)} rate=${(m.values.rate * 100).toFixed(3)}%  (${m.values.passes} of ${m.values.passes + m.values.fails})`);
      pick[name] = { rate: m.values.rate, passes: m.values.passes, fails: m.values.fails };
    } else if (m.type === 'counter' && (name === 'unexpected_status' || name.startsWith('n_') || name === 'http_reqs' || name === 'iterations')) {
      rows.push(`${name.padEnd(44)} count=${m.values.count}  rate=${m.values.rate.toFixed(2)}/s`);
      pick[name] = { count: m.values.count, rate: m.values.rate };
    }
    if (m.thresholds) {
      for (const [t, r] of Object.entries(m.thresholds)) rows.push(`  threshold ${name} ${t}: ${r.ok ? 'PASS' : 'FAIL'}`);
      pick[`${name}.thresholds`] = Object.fromEntries(Object.entries(m.thresholds).map(([t, r]) => [t, r.ok]));
    }
  }
  rows.sort();
  const text = `\n== ${scenario} (${new Date().toISOString()})\n${rows.join('\n')}\n`;
  const json = JSON.stringify({ scenario, at: new Date().toISOString(), state: data.state, metrics: pick, ...extra }, null, 2);
  return { stdout: text, [`${OUT}/${scenario}-summary.json`]: json };
}
