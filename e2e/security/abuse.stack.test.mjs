// W4-03 abuse suite, stack-only part: checks that need Caddy, the running API and the mocks server
// (`make dev`). Everything that can run in-process lives in Go instead, against testpg:
// backend/cmd/todayz/security_abuse_http_test.go (IDOR across stores, role escalation, foreign
// code routes, bill tokens, referral farming) and the identity, POS and order tests it names.
//
// Run (only against the local stack; `scripts/ci/scan.sh abuse --if-up` sets the Caddy CA):
//   NODE_EXTRA_CA_CERTS="$(caddy root cert)" node --test e2e/security/abuse.stack.test.mjs
//
// Zero dependencies (node:test and fetch), so it needs no workspace package.
// It prints routes, statuses and problem codes only: never a body, an OTP, a cookie or a phone.
// It spends three OTP sends from this machine's per-IP hourly cap (20) and two reserved test
// numbers' per-phone caps; run it after the journeys, or `make reset` first.

import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { describe, test } from 'node:test';

const URLS = {
  api: process.env.TZ_API_URL ?? 'https://api.todayz.localhost',
  mocks: process.env.TZ_MOCKS_URL ?? 'http://127.0.0.1:4010',
  customer: process.env.TZ_CUSTOMER_URL ?? 'https://app.todayz.localhost',
};

// Only the local stack, the same rule as scan.sh zap_target_ok: loopback or *.todayz.localhost.
const LOCAL = /^https?:\/\/(127\.0\.0\.1|localhost|\[::1\]|todayz\.localhost|[a-z0-9-]+\.todayz\.localhost)(:[0-9]{1,5})?$/;
for (const [name, url] of Object.entries(URLS)) {
  if (!LOCAL.test(url)) throw new Error(`refusing ${name}: not the local stack (loopback or *.todayz.localhost only)`);
}

// Reserved test numbers (01 §2, +91 90000000xx) that no seed or journey uses.
const PHONE_GAP = '+919000000087';
const PHONE_LOCK = '+919000000088';
const TURNSTILE = 'XXXX.DUMMY.TOKEN.XXXX';

const CROCKFORD = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';
function ulid() {
  let t = Date.now();
  let time = '';
  for (let i = 0; i < 10; i++) {
    time = CROCKFORD[t % 32] + time;
    t = Math.floor(t / 32);
  }
  return time + [...randomBytes(16)].map((b) => CROCKFORD[b % 32]).join('');
}

async function problemCode(r) {
  const body = await r.json().catch(() => ({}));
  return typeof body.code === 'string' ? body.code : '';
}

function post(path, data, headers = {}) {
  return fetch(`${URLS.api}${path}`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', ...headers },
    body: JSON.stringify(data),
    redirect: 'manual',
  });
}

function sendOtp(phone) {
  return post('/v1/auth/otp/send', { phone, purpose: 'login', app: 'customer', turnstileToken: TURNSTILE });
}

async function latestOtp(phone, since) {
  const q = new URLSearchParams({ to: phone, since, wait: '8' });
  const r = await fetch(`${URLS.mocks}/_mock/sms/latest-otp?${q}`);
  assert.equal(r.status, 200, 'the mocks server returned the SMS code');
  return (await r.json()).otp;
}

describe('API headers through Caddy', () => {
  test('every API answer carries the deny-all CSP, nosniff and no-store (slot 2)', async () => {
    for (const path of ['/v1/me', '/v1/does-not-exist']) {
      const r = await fetch(`${URLS.api}${path}`);
      assert.ok(r.status >= 400, `${path} is refused without a token`);
      assert.equal(r.headers.get('x-content-type-options'), 'nosniff', path);
      assert.match(r.headers.get('content-security-policy') ?? '', /default-src 'none'.*frame-ancestors 'none'/, path);
      assert.equal(r.headers.get('cache-control'), 'no-store', path);
      assert.equal(r.headers.get('referrer-policy'), 'no-referrer', path);
    }
  });

  test('a foreign origin gets no CORS grant on a preflight', async () => {
    const r = await fetch(`${URLS.api}/v1/me`, {
      method: 'OPTIONS',
      headers: { origin: 'https://evil.example', 'access-control-request-method': 'GET', 'access-control-request-headers': 'authorization' },
    });
    assert.equal(r.headers.get('access-control-allow-origin'), null, 'no ACAO for a foreign origin');
    assert.equal(r.headers.get('access-control-allow-credentials'), null);
  });

  test('a missing or forged bearer is 401', async () => {
    assert.equal((await fetch(`${URLS.api}/v1/me`)).status, 401);
    const forged = await fetch(`${URLS.api}/v1/me`, { headers: { authorization: 'Bearer eyJhbGciOiJub25lIn0.eyJzdWIiOiJ4In0.' } });
    assert.equal(forged.status, 401, 'an alg=none token is refused');
  });
});

describe('refresh CSRF through Caddy (D-37)', () => {
  const cookie = { cookie: `__Secure-tz_rt_c=${randomBytes(24).toString('base64url')}` };
  for (const [name, headers] of Object.entries({
    'foreign origin': { ...cookie, origin: 'https://evil.example', 'x-tz-client': 'customer-pwa/1.0' },
    'another app': { ...cookie, origin: 'https://business.todayz.localhost', 'x-tz-client': 'customer-pwa/1.0' },
    'no origin': { ...cookie, 'x-tz-client': 'customer-pwa/1.0' },
    'no X-TZ-Client': { ...cookie, origin: URLS.customer },
  })) {
    test(`${name}: 403 and no Set-Cookie`, async () => {
      const r = await post('/v1/auth/refresh', { app: 'customer' }, headers);
      assert.equal(r.status, 403, name);
      assert.equal(r.headers.get('set-cookie'), null, `${name}: a refused origin leaves the cookie`);
    });
  }

  test('the right origin with an unknown cookie is 401', async () => {
    const r = await post('/v1/auth/refresh', { app: 'customer' }, { ...cookie, origin: URLS.customer, 'x-tz-client': 'customer-pwa/1.0' });
    assert.equal(r.status, 401);
  });
});

describe('OTP limits on the stack (01 §2)', () => {
  test('a second send inside the resend gap is 429 with Retry-After', async () => {
    const first = await sendOtp(PHONE_GAP);
    assert.ok(first.status === 200 || first.status === 429, `first send answered ${first.status}`);
    const second = await sendOtp(PHONE_GAP);
    assert.equal(second.status, 429, 'the resend gap holds');
    assert.equal(await problemCode(second), 'rate_limited');
    const wait = Number(second.headers.get('retry-after') ?? '0');
    assert.ok(wait > 0 && wait <= 3600, 'Retry-After is set');
  });

  test('five wrong codes lock the request: the right code is refused after', async () => {
    const since = new Date(Date.now() - 1000).toISOString();
    const s = await sendOtp(PHONE_LOCK);
    assert.equal(s.status, 200, `send answered ${s.status} (an hourly cap is a reason to make reset)`);
    const { requestId } = await s.json();
    const right = await latestOtp(PHONE_LOCK, since);
    const wrong = right === '123456' ? '654321' : '123456';
    const deviceId = ulid();
    for (let i = 0; i < 5; i++) {
      const v = await post('/v1/auth/otp/verify', { requestId, code: wrong, app: 'customer', deviceId });
      assert.ok([401, 423, 429].includes(v.status), `wrong code ${i + 1} answered ${v.status}`);
    }
    const v = await post('/v1/auth/otp/verify', { requestId, code: right, app: 'customer', deviceId });
    assert.ok(v.status >= 400, 'the right code no longer signs in');
    assert.ok(['otp_locked', 'otp_expired'].includes(await problemCode(v)), 'locked, not unauthorized');
    assert.equal(v.headers.get('set-cookie'), null, 'no refresh cookie');
  });
});
