// The per-app device id sent with OTP verify (deviceId, a ULID). It names this browser profile for
// the session list; it is not a secret and not a token. localStorage may be missing or throw
// (private window, blocked site data): then a fresh id is made for this page load.
import type { AuthApp } from './session';

const CROCKFORD = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

/** A ULID (26 Crockford characters; first character 0-7 so it fits 128 bits). */
export function newUlid(now: number = Date.now()): string {
  let t = now;
  let time = '';
  for (let i = 0; i < 10; i += 1) {
    time = CROCKFORD[t % 32] + time;
    t = Math.floor(t / 32);
  }
  const bytes = new Uint8Array(16);
  globalThis.crypto.getRandomValues(bytes);
  let rand = '';
  for (let i = 0; i < 16; i += 1) rand += CROCKFORD[(bytes[i] ?? 0) % 32];
  return time + rand;
}

const keyFor = (app: AuthApp) => `todayz.${app}.device`;

export function getDeviceId(app: AuthApp, storage?: Pick<Storage, 'getItem' | 'setItem'> | null): string {
  let store: Pick<Storage, 'getItem' | 'setItem'> | null = null;
  try {
    store = storage === undefined ? globalThis.localStorage : storage;
  } catch {
    store = null;
  }
  try {
    const existing = store?.getItem(keyFor(app));
    if (existing && /^[0-9A-HJKMNP-TV-Z]{26}$/.test(existing)) return existing;
  } catch {
    // fall through to a new id
  }
  const id = newUlid();
  try {
    store?.setItem(keyFor(app), id);
  } catch {
    // not persisted: the next load makes another
  }
  return id;
}
