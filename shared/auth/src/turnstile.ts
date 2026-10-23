// Cloudflare Turnstile for OTP send (AU-04). Local development uses Cloudflare's published
// always-pass test site key, for which no script is loaded and the dummy token is sent (the
// mock siteverify accepts it). A production build must carry a real site key: apps call
// `requireSiteKey` from vite.config so the build fails without one.

/** Cloudflare's public always-pass test site key. */
export const TURNSTILE_TEST_SITE_KEY = '1x00000000000000000000AA';
export const TURNSTILE_DUMMY_TOKEN = 'XXXX.DUMMY.TOKEN.XXXX';

const TEST_KEY = /^[123]x0{20,}[A-Za-z]{2}$/;

export function isTestSiteKey(key: string): boolean {
  return TEST_KEY.test(key);
}

/** For vite.config: the key to bake in, or a thrown error for a production build without a real one. */
export function requireSiteKey(mode: string, key: string | undefined): string {
  const value = key?.trim() ?? '';
  if (mode === 'production') {
    if (value === '' || isTestSiteKey(value)) {
      throw new Error('VITE_TURNSTILE_SITE_KEY must be a real Turnstile site key for a production build');
    }
    return value;
  }
  return value === '' ? TURNSTILE_TEST_SITE_KEY : value;
}

interface TurnstileApi {
  render(el: HTMLElement, opts: Record<string, unknown>): string;
  execute(id: string): void;
  remove(id: string): void;
}

let scriptPromise: Promise<TurnstileApi> | null = null;

function loadScript(): Promise<TurnstileApi> {
  const w = window as unknown as { turnstile?: TurnstileApi };
  if (w.turnstile) return Promise.resolve(w.turnstile);
  scriptPromise ??= new Promise<TurnstileApi>((resolve, reject) => {
    const s = document.createElement('script');
    s.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit';
    s.async = true;
    s.onload = () => (w.turnstile ? resolve(w.turnstile) : reject(new Error('turnstile')));
    s.onerror = () => {
      scriptPromise = null;
      reject(new Error('turnstile'));
    };
    document.head.appendChild(s);
  });
  return scriptPromise;
}

/** A token for one OTP send. Rejects when the challenge cannot load or fails. */
export async function getTurnstileToken(siteKey: string): Promise<string> {
  if (isTestSiteKey(siteKey)) return TURNSTILE_DUMMY_TOKEN;
  const api = await loadScript();
  return new Promise<string>((resolve, reject) => {
    const host = document.createElement('div');
    host.style.position = 'fixed';
    host.style.bottom = '8px';
    host.style.right = '8px';
    document.body.appendChild(host);
    let id = '';
    const done = () => {
      if (id) api.remove(id);
      host.remove();
    };
    id = api.render(host, {
      sitekey: siteKey,
      size: 'invisible',
      callback: (token: string) => {
        done();
        resolve(token);
      },
      'error-callback': () => {
        done();
        reject(new Error('turnstile'));
      },
    });
    api.execute(id);
  });
}
