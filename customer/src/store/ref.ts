// The QR deep link `/s/{slug}?r=CODE` (01 §3): the code is kept on this device under one key until sign-in
// and the first order can attribute it. TODO(W2-01): send it with the first order / sign-up; until the
// referral API lands nothing reads it. Never logged.
import { browserStore } from '@todayz/i18n';

const KEY = 'todayz.ref';

export function rememberRef(code: string | undefined): void {
  if (!code) return;
  try {
    browserStore()?.setItem(KEY, code);
  } catch {
    // storage blocked: the link still works, attribution is lost
  }
}

export function readRef(): string | null {
  try {
    return browserStore()?.getItem(KEY) ?? null;
  } catch {
    return null;
  }
}
