import { chmodSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { expect, request as playwrightRequest, type APIRequestContext, type BrowserContext } from '@playwright/test';

import { Api, adminTotpSecret, totp, ulid, type Bearer } from './api';
import { URLS, type App, type Person } from './seed';

// Each person signs in once per gate run: the API allows 20 OTP sends per client IP per hour and 5 per
// phone (01 §2), and every local run comes from 127.0.0.1. The session (access token and refresh
// cookie) is kept in a file only this user can read, under the OS temp dir (never the repo), so a
// Playwright worker that restarts after a failure does not sign in again. globalTeardown deletes it.

const COOKIE: Record<App, string> = { customer: '__Secure-tz_rt_c', business: '__Secure-tz_rt_b', admin: '__Secure-tz_rt_a' };
const ORIGIN: Record<App, string> = { customer: URLS.customer, business: URLS.business, admin: URLS.admin };
const CLIENT = 'e2e-journeys/1.0.0';
/** Renew when the token has less than this left on the server's clock. */
const LEAD_MS = 90_000;

interface Cookie {
  name: string;
  value: string;
  domain: string;
  path: string;
  expires: number;
  httpOnly: boolean;
  secure: boolean;
  sameSite: 'Strict' | 'Lax' | 'None';
}

interface Saved {
  token: string;
  /** Expiry on the server's clock (the test clock may run ahead of this machine's). */
  expiresAt: number;
  userId: string;
  isNewUser: boolean;
  cookies: Cookie[];
  /** An admin's last accepted TOTP step: the server takes each 30 s step once. */
  totpStep?: number;
}

interface State {
  /** The test clock's offset from real time, as the last clock call reported it. */
  clockOffsetMs: number;
  people: Record<string, Saved>;
}

function stateDir(): string {
  const run = process.env['TZ_JOURNEYS_RUN'];
  if (!run) throw new Error('TZ_JOURNEYS_RUN is not set: run the journeys through playwright.config.ts');
  return join(tmpdir(), `todayz-journeys-${run}`);
}

function stateFile(): string {
  return join(stateDir(), 'state.json');
}

export function readState(): State {
  const f = stateFile();
  if (!existsSync(f)) return { clockOffsetMs: 0, people: {} };
  return JSON.parse(readFileSync(f, 'utf8')) as State;
}

function writeState(s: State) {
  const dir = stateDir();
  mkdirSync(dir, { recursive: true, mode: 0o700 });
  const f = stateFile();
  writeFileSync(f, JSON.stringify(s), { mode: 0o600 });
  chmodSync(f, 0o600);
}

export function setClockOffset(ms: number) {
  const s = readState();
  s.clockOffsetMs = ms;
  writeState(s);
}

export function removeState() {
  rmSync(stateDir(), { recursive: true, force: true });
}

async function newRequest(cookies: Cookie[] = []): Promise<APIRequestContext> {
  return playwrightRequest.newContext({ ignoreHTTPSErrors: true, storageState: { cookies, origins: [] } });
}

async function latestOtp(request: APIRequestContext, national: string, since: string): Promise<string> {
  const r = await request.get(`${URLS.mocks}/_mock/sms/latest-otp`, { params: { to: `+91${national}`, since, wait: '8' } });
  expect(r.status(), 'the mocks server returned the SMS code').toBe(200);
  return ((await r.json()) as { otp: string }).otp;
}

async function sendOtp(request: APIRequestContext, person: Person): Promise<{ requestId: string; since: string }> {
  for (let attempt = 0; ; attempt++) {
    const since = new Date(Date.now() - 1000).toISOString();
    const r = await request.post(`${URLS.api}/v1/auth/otp/send`, {
      data: { phone: `+91${person.national}`, purpose: 'login', app: person.app, turnstileToken: 'XXXX.DUMMY.TOKEN.XXXX' },
    });
    if (r.status() === 200) return { requestId: ((await r.json()) as { requestId: string }).requestId, since };
    const wait = Number(r.headers()['retry-after'] ?? '0');
    // The 30 s gap between two sends to one phone: wait it out once. The hourly caps are not waited for.
    if (r.status() === 429 && wait <= 35 && attempt === 0) {
      await new Promise((done) => setTimeout(done, wait * 1000 + 500));
      continue;
    }
    throw new Error(`OTP send for ${person.key} answered ${r.status()} (retry-after ${wait}s); the per-IP or per-phone hourly cap is a reason to make reset`);
  }
}

/**
 * One OTP sign-in attempt that keeps no session: the verify's status and problem code (a blocked
 * account answers 403 account_blocked). Uses one of the phone's OTP sends.
 */
export async function trySignIn(person: Person): Promise<{ status: number; code: string }> {
  const request = await newRequest();
  try {
    const { requestId, since } = await sendOtp(request, person);
    const code = await latestOtp(request, person.national, since);
    const v = await request.post(`${URLS.api}/v1/auth/otp/verify`, { data: { requestId, code, app: person.app, deviceId: ulid() } });
    const body = (await v.json().catch(() => ({}))) as { code?: string };
    return { status: v.status(), code: v.status() >= 400 ? (body.code ?? '') : '' };
  } finally {
    await request.dispose();
  }
}

/** One person's session. Use `api` for calls; `attach` lends the session to a browser context. */
export class Principal implements Bearer {
  readonly api: Api;
  private ctx: APIRequestContext | undefined;
  private lent: BrowserContext | undefined;
  private saved: Saved | undefined;

  constructor(readonly person: Person) {
    this.api = new Api(this);
  }

  get userId(): string {
    if (!this.saved) throw new Error(`${this.person.key} is not signed in`);
    return this.saved.userId;
  }

  get isNewUser(): boolean {
    return this.saved?.isNewUser ?? false;
  }

  request(): APIRequestContext {
    if (this.lent) return this.lent.request;
    if (!this.ctx) throw new Error(`${this.person.key} has no request context`);
    return this.ctx;
  }

  /** Signs in (OTP from the mocks, plus TOTP for an admin) unless a saved session exists. */
  async open(): Promise<this> {
    const s = readState().people[this.person.key];
    if (s) {
      this.saved = s;
      this.ctx = await newRequest(s.cookies);
      return this;
    }
    this.ctx = await newRequest();
    const { requestId, since } = await sendOtp(this.ctx, this.person);
    const code = await latestOtp(this.ctx, this.person.national, since);
    const v = await this.ctx.post(`${URLS.api}/v1/auth/otp/verify`, { data: { requestId, code, app: this.person.app, deviceId: ulid() } });
    expect(v.status(), `OTP verify for ${this.person.key}`).toBe(200);
    const body = (await v.json()) as { accessToken: string; expiresAt: string; userId: string; isNewUser: boolean; nextStep?: string };
    this.saved = { token: body.accessToken, expiresAt: Date.parse(body.expiresAt), userId: body.userId, isNewUser: body.isNewUser, cookies: [] };
    if (this.person.app === 'admin') {
      expect(body.nextStep, `${this.person.key} is an enrolled admin`).toBe('totp_verify');
      await this.totpVerify('login');
    }
    await this.persist();
    return this;
  }

  /**
   * A fresh authenticator code for a † admin action (D-64): the API stamps this sign-in as checked
   * for 10 minutes and answers a new access token.
   */
  async stepUp(): Promise<void> {
    if (this.person.app !== 'admin') throw new Error(`${this.person.key} is not an admin`);
    await this.totpVerify('step_up');
    await this.persist();
  }

  private async totpVerify(purpose: 'login' | 'step_up'): Promise<void> {
    if (!this.saved || !this.ctx) throw new Error(`${this.person.key} is not signed in`);
    const secret = adminTotpSecret(this.person.admin ?? '');
    const wait = (ms: number) => new Promise((done) => setTimeout(done, ms));
    // Not in the last seconds of a 30 s step, so the code is still current when it arrives; and a
    // step after the last one used, since the server refuses a step twice.
    for (;;) {
      const now = Date.now();
      const left = 30_000 - (now % 30_000);
      const step = Math.floor(now / 30_000);
      if (left < 4_000 || (this.saved.totpStep !== undefined && step <= this.saved.totpStep)) {
        await wait(left + 300);
        continue;
      }
      const t = await this.request().post(`${URLS.api}/v1/auth/totp/verify`, {
        headers: { Authorization: `Bearer ${purpose === 'login' ? this.saved.token : await this.token()}` },
        data: { code: totp(secret, now), purpose },
      });
      expect(t.status(), `TOTP ${purpose} for ${this.person.key}`).toBe(200);
      const tb = (await t.json()) as { accessToken: string; expiresAt: string };
      this.saved.token = tb.accessToken;
      this.saved.expiresAt = Date.parse(tb.expiresAt);
      this.saved.totpStep = step;
      return;
    }
  }

  async token(): Promise<string> {
    if (!this.saved) throw new Error(`${this.person.key} is not signed in`);
    const serverNow = Date.now() + readState().clockOffsetMs;
    if (this.saved.expiresAt - serverNow < LEAD_MS) await this.renew();
    return this.saved.token;
  }

  async renew(): Promise<void> {
    const r = await this.request().post(`${URLS.api}/v1/auth/refresh`, {
      headers: { Origin: ORIGIN[this.person.app], 'X-TZ-Client': CLIENT },
      data: { app: this.person.app },
    });
    expect(r.status(), `session refresh for ${this.person.key}`).toBe(200);
    const b = (await r.json()) as { accessToken: string; expiresAt: string };
    if (!this.saved) throw new Error(`${this.person.key} is not signed in`);
    this.saved.token = b.accessToken;
    this.saved.expiresAt = Date.parse(b.expiresAt);
    await this.persist();
  }

  /**
   * Lends the session to a browser context: its refresh cookie goes into the context's jar and the
   * app's `hasSession` hint into its storage, so the app signs itself in with one refresh. Until
   * `detach`, API calls use the context's request (same cookie jar), so the page and the test never
   * hold different refresh tokens.
   */
  async attach(context: BrowserContext): Promise<void> {
    const cookies = await this.cookies();
    await context.addCookies(cookies);
    const key = `todayz.hasSession.${this.person.app}`;
    await context.addInitScript((k) => {
      try {
        localStorage.setItem(k, '1');
      } catch {
        // storage blocked: the app still tries the cookie when it cannot read the hint
      }
    }, key);
    this.lent = context;
  }

  /** Takes the session back from the browser context (call before the context closes). */
  async detach(): Promise<void> {
    if (!this.lent) return;
    // All of the jar, then by name: the refresh cookie's path is /v1/auth, so cookies(URLS.api) misses it.
    const cookies = (await this.lent.cookies()).filter((c) => c.name === COOKIE[this.person.app]) as Cookie[];
    this.lent = undefined;
    await this.ctx?.dispose();
    this.ctx = await newRequest(cookies);
    await this.persist();
  }

  private async cookies(): Promise<Cookie[]> {
    const st = await this.request().storageState();
    return (st.cookies as Cookie[]).filter((c) => c.name === COOKIE[this.person.app]);
  }

  private async persist() {
    if (!this.saved) return;
    this.saved.cookies = await this.cookies();
    const s = readState();
    s.people[this.person.key] = this.saved;
    writeState(s);
  }

  /** Forgets the saved session (after the account was deleted or blocked). */
  forget() {
    const s = readState();
    delete s.people[this.person.key];
    writeState(s);
  }

  async dispose() {
    await this.ctx?.dispose();
    this.ctx = undefined;
  }
}

const open = new Map<string, Principal>();

/** The signed-in principal for a person, signing in on first use in this run. */
export async function principal(person: Person): Promise<Principal> {
  let p = open.get(person.key);
  if (!p) {
    p = await new Principal(person).open();
    open.set(person.key, p);
  }
  return p;
}

export async function disposeAll() {
  for (const p of open.values()) await p.dispose();
  open.clear();
}
