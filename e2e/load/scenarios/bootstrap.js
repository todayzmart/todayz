// Signs in the load shop people once (business app) and names them, so data/link_staff.sql can
// make pool A the owners and pool B the managers of the generated stores. Run by make load-data
// after generate.sql; a rerun only renames (one OTP send per person).
import http from 'k6/http';
import exec from 'k6/execution';
import { fail } from 'k6';
import { API, POOL, POOL_SIZE, SOAK_POOL_SIZE, poolNational, signIn, summary, metrics, rec, headers } from '../lib/common.js';

const M = metrics({ me_get: 'read', me_patch: 'write' });

export const options = {
  scenarios: {
    people: { executor: 'shared-iterations', vus: 8, iterations: POOL_SIZE * 2 + SOAK_POOL_SIZE, maxDuration: '10m' },
  },
  thresholds: { err_all: ['rate==0'] },
};

export default function () {
  const i = exec.scenario.iterationInTest; // 0 … 104
  let pool = 'owner';
  let n = i + 1;
  if (i >= POOL_SIZE * 2) {
    pool = 'soak';
    n = i - POOL_SIZE * 2 + 1;
  } else if (i >= POOL_SIZE) {
    pool = 'manager';
    n = i - POOL_SIZE + 1;
  }
  const s = signIn(poolNational(pool, n), 'business');
  if (!s) fail(`${pool} ${n}: sign-in failed`);
  const me = http.get(`${API}/v1/me`, { headers: headers(s.token), tags: { name: 'me_get' } });
  rec(M, 'me_get', me);
  const name = `${POOL[pool].name} ${String(n).padStart(2, '0')}`;
  if (me.json('name') === name) return;
  const r = http.patch(`${API}/v1/me`, JSON.stringify({ name }), {
    headers: headers(s.token, { 'If-Match': me.headers.Etag || me.headers.ETag }),
    tags: { name: 'me_patch' },
  });
  rec(M, 'me_patch', r);
}

export function handleSummary(data) {
  return summary('bootstrap', data);
}
