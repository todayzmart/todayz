import en from '@todayz/i18n/compiled/business/en.json';
import { createSession, createUpdateController, type Session, type UpdateController } from '@todayz/auth';
import { asMessages, type Messages } from '@todayz/i18n';
import { createMemoryHistory } from '@tanstack/react-router';
import { render } from '@testing-library/react';
import { App } from '../app/App';
import { makeQueryClient, makeRouter } from '../app/router';

export interface Call {
  method: string;
  path: string;
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
  /** Answers per request; the first handler that returns a Response wins. */
  handlers?: Handler[];
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
      headers: new Headers(init?.headers),
      body: raw ? (JSON.parse(raw) as unknown) : undefined,
    };
    calls.push(call);
    for (const h of handlers) {
      const r = h(call);
      if (r) return r;
    }
    if (call.path === '/v1/auth/refresh') return problem(401, 'unauthorized');
    return problem(404, 'not_found');
  }) as typeof fetch;

  const session: Session = createSession({
    app: 'business',
    baseUrl: 'https://api.test',
    clientId: 'business/test',
    fetch: fakeFetch,
    locks: null,
    channel: null,
    graceWaitMs: 0,
  });
  if (opts.signedIn !== false) {
    session.signIn({ accessToken: 'test-token', expiresAt: new Date(Date.now() + 3_600_000).toISOString(), userId: 'u1' });
  }
  const update: UpdateController = createUpdateController();
  const queryClient = makeQueryClient();
  const history = createMemoryHistory({ initialEntries: [path] });
  const router = makeRouter(queryClient, history);
  const view = render(
    <App messages={opts.messages ?? asMessages(en)} router={router} queryClient={queryClient} session={session} update={update} />,
  );
  return { ...view, history, router, session, update, calls, queryClient };
}
