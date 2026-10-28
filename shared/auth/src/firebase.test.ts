import { describe, expect, it } from 'vitest';
import { FirebasePhoneError, firebaseErrorKind } from './firebase';

describe('firebaseErrorKind', () => {
  it('maps Firebase Auth codes to the kinds the screen shows', () => {
    const cases: [string, string][] = [
      ['auth/invalid-verification-code', 'wrongCode'],
      ['auth/code-expired', 'expired'],
      ['auth/too-many-requests', 'rateLimited'],
      ['auth/quota-exceeded', 'rateLimited'],
      ['auth/invalid-phone-number', 'invalidPhone'],
      ['auth/captcha-check-failed', 'captcha'],
      ['auth/invalid-app-credential', 'captcha'],
      ['auth/network-request-failed', 'network'],
      ['auth/user-disabled', 'blocked'],
      ['auth/internal-error', 'generic'],
    ];
    for (const [code, kind] of cases) expect(firebaseErrorKind({ code })).toBe(kind);
  });

  it('keeps a kind already reduced and treats anything else as generic', () => {
    expect(firebaseErrorKind(new FirebasePhoneError('expired'))).toBe('expired');
    expect(firebaseErrorKind(new Error('x'))).toBe('generic');
    expect(firebaseErrorKind(null)).toBe('generic');
  });
});
