import { chmodSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { expect, request as playwrightRequest, type APIRequestContext, type BrowserContext } from '@playwright/test';

import { Api, ulid, type Bearer } from './api';
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
  /**
   * When this admin's sign-in last passed the SMS step-up (D-264), on the server's clock. Refresh keeps
   * the stamp on the sign-in, so it lasts until a new sign-in.
   */
  stepUpAt?: number;
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

/** The newest SMS code the mocks hold for a number sent since `since` (a code another flow sent, such as the field owner confirm). Never print it. */
export async function smsCodeSince(national: string, since: string): Promise<string> {
  const request = await newRequest();
  try {
    return await latestOtp(request, national, since);
  } finally {
    await request.dispose();
  }
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

/** The admin SMS step-up send (D-264): the API texts the signed-in admin's own number; the gap rule is sendOtp's. */
async function sendStepUp(request: APIRequestContext, person: Person, bearer: string): Promise<{ requestId: string; since: string }> {
  for (let attempt = 0; ; attempt++) {
    const since = new Date(Date.now() - 1000).toISOString();
    const r = await request.post(`${URLS.api}/v1/auth/step-up/send`, { headers: { Authorization: `Bearer ${bearer}` } });
    if (r.status() === 200) return { requestId: ((await r.json()) as { requestId: string }).requestId, since };
    const wait = Number(r.headers()['retry-after'] ?? '0');
    if (r.status() === 429 && wait <= 35 && attempt === 0) {
      await new Promise((done) => setTimeout(done, wait * 1000 + 500));
      continue;
    }
    throw new Error(`step-up send for ${person.key} answered ${r.status()} (retry-after ${wait}s); the per-IP or per-phone hourly cap is a reason to make reset`);
  }
}

/** A step-up younger than this (server clock) is reused; the API's window is 10 minutes (authn.StepUpWindow). */
const STEP_UP_REUSE_MS = 8 * 60_000;

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

  /** Signs in (OTP from the mocks; for an admin too, D-264) unless a saved session exists. */
  async open(): Promise<this> {
    const s = readState().people[this.person.key];
    if (s) {
      this.saved = s;
      this.ctx = await newRequest(s.cookies);
      return this;
    }
    this.ctx = await newRequest();
    await this.signIn();
    return this;
  }

  /** OTP on this person's own request context; keeps the session. The code alone signs an admin in (D-264). */
  private async signIn(): Promise<void> {
    if (!this.ctx) throw new Error(`${this.person.key} has no request context`);
    const { requestId, since } = await sendOtp(this.ctx, this.person);
    const code = await latestOtp(this.ctx, this.person.national, since);
    const v = await this.ctx.post(`${URLS.api}/v1/auth/otp/verify`, { data: { requestId, code, app: this.person.app, deviceId: ulid() } });
    expect(v.status(), `OTP verify for ${this.person.key}`).toBe(200);
    const body = (await v.json()) as { accessToken: string; expiresAt: string; userId: string; isNewUser: boolean };
    expect(body, `${this.person.key}: the SMS code alone signs in, no second step (D-264)`).not.toHaveProperty('nextStep');
    // A new sign-in starts without a step-up.
    this.saved = { token: body.accessToken, expiresAt: Date.parse(body.expiresAt), userId: body.userId, isNewUser: body.isNewUser, cookies: [] };
    await this.persist();
  }

  /**
   * A fresh SMS step-up for a † admin action (D-264): the API texts a code to this admin's own number,
   * the code comes from the mocks server, and a right code stamps this sign-in as checked for 10
   * minutes (the token does not change). A step-up younger than STEP_UP_REUSE_MS is reused, since
   * every send counts toward the phone's and the IP's hourly caps; `force` skips that (after a
   * `403 step_up_required`).
   */
  async stepUp(opts: { force?: boolean } = {}): Promise<void> {
    if (this.person.app !== 'admin') throw new Error(`${this.person.key} is not an admin`);
    if (!this.saved) throw new Error(`${this.person.key} is not signed in`);
    if (!opts.force && this.saved.stepUpAt !== undefined && this.serverNow() - this.saved.stepUpAt < STEP_UP_REUSE_MS) return;
    const bearer = await this.token();
    const { requestId, since } = await sendStepUp(this.request(), this.person, bearer);
    const code = await latestOtp(this.request(), this.person.national, since);
    const t = await this.request().post(`${URLS.api}/v1/auth/step-up/verify`, {
      headers: { Authorization: `Bearer ${bearer}` },
      data: { requestId, code },
    });
    expect(t.status(), `step-up verify for ${this.person.key}`).toBe(200);
    await this.noteStepUp();
  }

  /** Records a step-up passed on this sign-in, here or in the console's dialog on a lent session. */
  async noteStepUp(): Promise<void> {
    if (!this.saved) return;
    this.saved.stepUpAt = this.serverNow();
    await this.persist();
  }

  /** The newest SMS code sent to this person since `since` (ISO time). Never print it. */
  async smsCode(since: string): Promise<string> {
    return latestOtp(this.request(), this.person.national, since);
  }

  private serverNow(): number {
    return Date.now() + readState().clockOffsetMs;
  }

  async token(): Promise<string> {
    if (!this.saved) throw new Error(`${this.person.key} is not signed in`);
    const serverNow = Date.now() + readState().clockOffsetMs;
    if (this.saved.expiresAt - serverNow < LEAD_MS) await this.renew();
    return this.saved.token;
  }

  async renew(): Promise<void> {
    const refresh = () =>
      this.request().post(`${URLS.api}/v1/auth/refresh`, {
        headers: { Origin: ORIGIN[this.person.app], 'X-TZ-Client': CLIENT },
        data: { app: this.person.app },
      });
    let r = await refresh();
    if (r.status() === 401 && this.lent) {
      // The page refreshed with the same cookie at the same moment and won: the server refuses the
      // loser inside RotationGrace and keeps the cookie, and the page's response puts the new one in
      // the shared jar. Wait for it and retry once, as shared/auth's session does (graceWaitMs).
      await new Promise((done) => setTimeout(done, 400));
      r = await refresh();
    }
    if (r.status() === 401 && !this.lent && this.person.app === 'admin') {
      // An admin's refresh token lives 12 h (01 §2); the clock journeys move the test clock past it.
      // Sign in again, as the admin would.
      await this.ctx?.dispose();
      this.ctx = await newRequest();
      await this.signIn();
      return;
    }
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
