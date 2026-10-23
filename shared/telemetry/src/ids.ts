// Client-side ids: ULIDs for events, the anonymous id and the session id, kept in browser storage that may
// be missing (private window, blocked site data), so every read and write is guarded.

const CROCKFORD = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

function randomBytes(n: number): Uint8Array {
  const buf = new Uint8Array(n);
  if (typeof crypto !== 'undefined' && typeof crypto.getRandomValues === 'function') {
    crypto.getRandomValues(buf);
  } else {
    for (let i = 0; i < n; i++) buf[i] = Math.floor(Math.random() * 256);
  }
  return buf;
}

/** A ULID: 10 characters of millisecond time, 16 of randomness. Matches `^[0-7][0-9A-HJKMNP-TV-Z]{25}$`. */
export function newUlid(nowMs: number = Date.now()): string {
  let t = Math.max(0, Math.floor(nowMs));
  let time = '';
  for (let i = 0; i < 10; i++) {
    time = CROCKFORD.charAt(t % 32) + time;
    t = Math.floor(t / 32);
  }
  let rand = '';
  for (const b of randomBytes(16)) rand += CROCKFORD.charAt(b & 31);
  return time + rand;
}

type StorageLike = Pick<Storage, 'getItem' | 'setItem'>;

const memory = new Map<string, string>();

/** Reads `key`, or creates and stores a ULID under it. Falls back to memory when storage throws. */
export function persistentId(storage: () => StorageLike | undefined, key: string): string {
  try {
    const s = storage();
    const existing = s?.getItem(key);
    if (existing && /^[0-7][0-9A-HJKMNP-TV-Z]{25}$/.test(existing)) return existing;
    const fresh = newUlid();
    s?.setItem(key, fresh);
    if (s) return fresh;
  } catch {
    // fall through to memory
  }
  const cached = memory.get(key);
  if (cached) return cached;
  const fresh = newUlid();
  memory.set(key, fresh);
  return fresh;
}

const localStore = (): StorageLike | undefined => (typeof localStorage === 'undefined' ? undefined : localStorage);
const sessionStore = (): StorageLike | undefined => (typeof sessionStorage === 'undefined' ? undefined : sessionStorage);

/** The random first-party id used before sign-in (05 §2.6); the server merges it to the user at login. */
export const anonId = (): string => persistentId(localStore, 'tz_anon');
/** One id per browser tab session. */
export const sessionId = (): string => persistentId(sessionStore, 'tz_sid');
