// `?next=` after sign-in, shared by the three apps (W1-08): where to go once signed in. It must be a
// path on this app and nothing else, because the value comes from the address bar.
const BASE = 'https://app.invalid';
/** C0 and C1 control characters (tab, CR and LF included). */
function hasControl(value: string): boolean {
  for (let i = 0; i < value.length; i++) {
    const c = value.charCodeAt(i);
    if (c <= 0x1f || (c >= 0x7f && c <= 0x9f)) return true;
  }
  return false;
}

/**
 * The relative target (`/orders?x=1`) for a `next` value, or '/' when it is not safe: not a string,
 * contains a control character or a backslash (browsers strip or fold them, so `/\t/evil.com` and
 * `/\evil.com` can mean another host), does not start with a single '/', does not resolve to this
 * origin, or points at one of the app's own gate routes (`blocked`, by path segment) which would loop.
 */
export function safeNext(next: unknown, blocked: readonly string[] = []): string {
  if (typeof next !== 'string' || next === '') return '/';
  if (hasControl(next) || next.includes('\\')) return '/';
  if (!next.startsWith('/') || next.startsWith('//')) return '/';
  let url: URL;
  try {
    url = new URL(next, BASE);
  } catch {
    return '/';
  }
  if (url.origin !== BASE) return '/';
  const path = url.pathname;
  if (blocked.some((b) => path === b || path.startsWith(`${b}/`))) return '/';
  return path + url.search + url.hash;
}

/** TanStack Router `validateSearch` for a route that takes `?next=`. */
export function validateNextSearch(search: Record<string, unknown>, blocked: readonly string[] = []): { next?: string } {
  const next = safeNext(search['next'], blocked);
  return next === '/' ? {} : { next };
}
