// Sentry seam (05 §2.4, D-17). No Sentry SDK is a dependency of this package: the app hands one in
// (`@sentry/browser`'s `init` and `captureException`) in Wave 5. Locally and in tests there is no DSN and no
// SDK, so nothing is initialised and nothing is sent. The user context is the user id and role only.

import type { SendEnv } from './allow';

export interface SentryLike {
  init(options: SentryInitOptions): void;
  captureException(error: unknown, context?: { extra?: Record<string, string> }): void;
  setUser?(user: { id: string } | null): void;
}

export interface SentryInitOptions {
  dsn: string;
  environment: string;
  release?: string | undefined;
  sendDefaultPii: false;
  beforeSend: <T extends SentryEventLike>(event: T) => T | null;
}

/** The slice of a Sentry event the scrubber touches. */
export interface SentryEventLike {
  message?: string;
  user?: { id?: string; [k: string]: unknown };
  request?: { url?: string; query_string?: unknown; cookies?: unknown; headers?: unknown; [k: string]: unknown };
  exception?: { values?: { value?: string; [k: string]: unknown }[] };
  breadcrumbs?: { message?: string; data?: unknown; [k: string]: unknown }[];
  [k: string]: unknown;
}

/** Sentry runs only in staging and production, with a DSN and an injected SDK. */
export function sentryAllowed(env: SendEnv, dsn: string | undefined, sdk: SentryLike | undefined): boolean {
  return Boolean(dsn && sdk && (env === 'staging' || env === 'production'));
}

/**
 * `beforeSend`: user context becomes the id only, request cookies, headers and query strings are dropped,
 * and messages and breadcrumbs pass through `scrub`.
 */
export function scrubSentryEvent<T extends SentryEventLike>(event: T, scrub: (text: string) => string): T {
  const out: SentryEventLike = { ...event };
  if (out.user) out.user = out.user.id ? { id: out.user.id } : {};
  if (out.request) {
    const { url } = out.request;
    out.request = url ? { url: scrub(url.split('?')[0] ?? '') } : {};
  }
  if (typeof out.message === 'string') out.message = scrub(out.message);
  if (out.exception?.values) {
    out.exception = {
      ...out.exception,
      values: out.exception.values.map((v) => (typeof v.value === 'string' ? { ...v, value: scrub(v.value) } : v)),
    };
  }
  if (out.breadcrumbs) {
    out.breadcrumbs = out.breadcrumbs.map((b) => {
      const { data: _data, ...rest } = b;
      return typeof rest.message === 'string' ? { ...rest, message: scrub(rest.message) } : rest;
    });
  }
  return out as T;
}
