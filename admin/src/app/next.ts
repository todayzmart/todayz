/** `?next=` after sign-in: a path on this app only, never another origin ("//x", "/\x", "https://x"). */
export function safeNext(next: unknown): string {
  if (typeof next !== 'string') return '/';
  if (!next.startsWith('/') || next.startsWith('//') || next.includes('\\')) return '/';
  if (next.startsWith('/sign-in') || next.startsWith('/two-step')) return '/';
  return next;
}

export function validateNextSearch(search: Record<string, unknown>): { next?: string } {
  const next = safeNext(search.next);
  return next === '/' ? {} : { next };
}
