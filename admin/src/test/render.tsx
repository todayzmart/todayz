import en from '@todayz/i18n/compiled/admin/en.json';
import { createSession, createUpdateController, type Session, type UpdateController } from '@todayz/auth';
import { asMessages, type Messages } from '@todayz/i18n';
import { createMemoryHistory } from '@tanstack/react-router';
import { render } from '@testing-library/react';
import { App } from '../app/App';
import { createSecondFactor, type SecondFactor } from '../app/second-factor';
import { makeQueryClient, makeRouter } from '../app/router';

export interface Call {
  method: string;
  path: string;
  /** The query string, `?a=b` or empty. */
  search: string;
  headers: Headers;
  body: unknown;
}

export type Handler = (call: Call) => Response | undefined;

export function json(status: number, body: unknown, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': status >= 400 ? 'application/problem+json' : 'application/json', ...headers },
  });
}

export function problem(status: number, code: string): Response {
  return json(status, { type: `about:blank#${code}`, title: code, status, code });
}

export interface RenderOptions {
  messages?: Messages;
  /** Start signed in (default) or signed out (the refresh answers 401). */
  signedIn?: boolean;
  /** Start with the authenticator step passed (default when signed in) or not. */
  secondFactor?: boolean;
  /** Answers per request; the first handler that returns a Response wins. */
  handlers?: Handler[];
  /** Admin roles `GET /v1/me` answers with when no handler does (default super_admin). */
  roles?: string[];
}

/** The `GET /v1/me` body for an admin with these roles. */
export function adminMe(roles: string[]) {
  return {
    id: '01JADMIN0000000000000000AD',
    name: 'Asha Rao',
    preferredLang: 'en',
    phoneMasked: '90•••••001',
    app: 'admin',
    roles,
    createdAt: '2026-10-01T04:30:00Z',
  };
}

/** Renders `<App>` at a path with a fake `fetch`. Signed in by default, so the shell and Overview tests stay simple. */
export function renderAt(path: string, opts: RenderOptions = {}) {
  const calls: Call[] = [];
  const handlers = opts.handlers ?? [];
  const fakeFetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url);
    const raw = typeof init?.body === 'string' ? init.body : undefined;
    const call: Call = {
      method: (init?.method ?? 'GET').toUpperCase(),
      path: url.pathname,
      search: url.search,
      headers: new Headers(init?.headers),
      body: raw ? (JSON.parse(raw) as unknown) : undefined,
    };
    calls.push(call);
    for (const h of handlers) {
      const r = h(call);
      if (r) return r;
    }
    if (call.path === '/v1/auth/refresh') return problem(401, 'unauthorized');
    if (call.path === '/v1/me' && call.method === 'GET') return json(200, adminMe(opts.roles ?? ['super_admin']));
    return problem(404, 'not_found');
  }) as typeof fetch;

  const session: Session = createSession({
    app: 'admin',
    baseUrl: 'https://api.test',
    clientId: 'admin/test',
    fetch: fakeFetch,
    locks: null,
    channel: null,
    graceWaitMs: 0,
  });
  if (opts.signedIn !== false) {
    session.signIn({ accessToken: 'test-token', expiresAt: new Date(Date.now() + 3_600_000).toISOString(), userId: 'u1' });
  }
  sessionStorage.clear();
  if (opts.signedIn !== false && opts.secondFactor !== false) sessionStorage.setItem('todayz.admin.2fa', 'test');
  const factor: SecondFactor = createSecondFactor();
  const update: UpdateController = createUpdateController();
  const queryClient = makeQueryClient();
  const history = createMemoryHistory({ initialEntries: [path] });
  const router = makeRouter(queryClient, history);
  const view = render(
    <App messages={opts.messages ?? asMessages(en)} router={router} queryClient={queryClient} session={session} update={update} factor={factor} />,
  );
  return { ...view, history, router, session, update, factor, calls };
}
