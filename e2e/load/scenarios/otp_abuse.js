// otp_abuse (08 §6): one client IP sending 100 OTPs a minute to fresh numbers, one number hammered
// from many IPs, and a code guesser, while legitimate people keep signing in from their own IPs.
// Pass: the abuser is blocked once the limits are reached (OTPPerIP = 20 sends/IP/hour, OTPPerPhone
// = 5/phone/hour with a 30 s gap, OTPMaxAttempts = 5 per code; 01 §2) and every legitimate
// sign-in succeeds.
//
// The generic per-route limiter (internal/platform/ratelimit) applies only in a deployed
// environment, so this run exercises the OTP rules themselves; see the W4-02 report for the
// limiter numbers recommended for the lead.
import http from 'k6/http';
import { Counter, Rate } from 'k6/metrics';
import { API, MOCKS, fakeIp, randomNational, signIn, ulid, summary, metrics, rec, STATS } from '../lib/common.js';

const ABUSER_IP = fakeIp(); // new per run, so an earlier run's hour does not count
const HAMMERED = randomNational();
const DURATION = __ENV.LOAD_OTP_DURATION || '3m';
const PER_IP = 20;
const PER_PHONE = 5;

const M = metrics({ abuse_send: 'write', hammer_send: 'write', guess_verify: 'write', legit_signin: 'write' });
const abuserAccepted = new Counter('n_abuser_sends_accepted');
const abuserBlocked = new Rate('ok_abuser_blocked_after_limit');
const hammerAccepted = new Counter('n_hammered_phone_sends_accepted');
const guessLocked = new Rate('ok_code_locked_after_max_attempts');
const legitOk = new Rate('ok_legit_signin');
let abuserSeen = 0; // per VU; the abuser runs on one VU

export const options = {
  summaryTrendStats: STATS,
  scenarios: {
    abuser: { executor: 'constant-arrival-rate', rate: 100, timeUnit: '1m', duration: DURATION, preAllocatedVUs: 1, maxVUs: 1, exec: 'abuser' },
    hammer: { executor: 'constant-arrival-rate', rate: 20, timeUnit: '1m', duration: DURATION, preAllocatedVUs: 2, maxVUs: 4, exec: 'hammer' },
    guesser: { executor: 'constant-arrival-rate', rate: 4, timeUnit: '1m', duration: DURATION, preAllocatedVUs: 2, maxVUs: 4, exec: 'guesser' },
    legit: { executor: 'constant-arrival-rate', rate: 30, timeUnit: '1m', duration: DURATION, preAllocatedVUs: 10, maxVUs: 30, exec: 'legit' },
  },
  thresholds: {
    n_abuser_sends_accepted: [`count<=${PER_IP}`],
    ok_abuser_blocked_after_limit: ['rate==1'],
    n_hammered_phone_sends_accepted: [`count<=${PER_PHONE}`],
    ok_code_locked_after_max_attempts: ['rate==1'],
    ok_legit_signin: ['rate==1'],
  },
};

function send(phone, ip, name) {
  return http.post(`${API}/v1/auth/otp/send`, JSON.stringify({ phone: `+91${phone}`, purpose: 'login', app: 'customer', turnstileToken: 'XXXX.DUMMY.TOKEN.XXXX' }), {
    headers: { 'Content-Type': 'application/json', 'CF-Connecting-IP': ip },
    tags: { name },
  });
}

export function abuser() {
  const r = send(randomNational(), ABUSER_IP, 'abuse_send');
  rec(M, 'abuse_send', r, [200, 429]);
  abuserSeen++;
  if (r.status === 200) abuserAccepted.add(1);
  if (abuserSeen > PER_IP) abuserBlocked.add(r.status === 429);
}

export function hammer() {
  const r = send(HAMMERED, fakeIp(), 'hammer_send');
  rec(M, 'hammer_send', r, [200, 429]);
  if (r.status === 200) hammerAccepted.add(1);
}

/** Five wrong codes on one request, then the right one: the right one must be refused too. */
export function guesser() {
  const phone = randomNational();
  const ip = fakeIp();
  const since = new Date(Date.now() - 1000).toISOString();
  const s = send(phone, ip, 'abuse_send');
  if (!rec(M, 'abuse_send', s, [200])) return;
  const requestId = s.json('requestId');
  const h = { headers: { 'Content-Type': 'application/json', 'CF-Connecting-IP': ip }, tags: { name: 'guess_verify' } };
  const o = http.get(`${MOCKS}/_mock/sms/latest-otp?to=${encodeURIComponent(`+91${phone}`)}&since=${encodeURIComponent(since)}&wait=8`, { tags: { name: 'mock_otp' } });
  const right = o.json('otp');
  for (let i = 0; i < 5; i++) {
    let wrong = String(Math.floor(Math.random() * 1e6)).padStart(6, '0');
    if (wrong === right) wrong = wrong === '111111' ? '222222' : '111111';
    const v = http.post(`${API}/v1/auth/otp/verify`, JSON.stringify({ requestId, code: wrong, app: 'customer', deviceId: ulid() }), h);
    rec(M, 'guess_verify', v, [400, 401, 403, 409, 410, 422, 429]);
  }
  const last = http.post(`${API}/v1/auth/otp/verify`, JSON.stringify({ requestId, code: right, app: 'customer', deviceId: ulid() }), h);
  rec(M, 'guess_verify', last, [400, 401, 403, 409, 410, 422, 429]);
  guessLocked.add(last.status !== 200);
}

export function legit() {
  const t0 = Date.now();
  const s = signIn(randomNational(), 'customer');
  legitOk.add(!!s);
  M.route.legit_signin.add(Date.now() - t0);
  M.kind.write.add(Date.now() - t0);
}

export function handleSummary(data) {
  return summary('otp_abuse', data);
}
