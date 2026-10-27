// Firebase phone sign-in for PhoneOtpFlow (D-275): when the API's OTP_PROVIDER is firebase,
// Firebase Auth sends and checks the code in the page and the flow swaps the Firebase ID token for
// a TodayZ session. The SDK sits in its own chunk (firebase-sdk.ts, loaded with import()), so a
// page on the SMS path never downloads it. Nothing here logs the phone, the code or a token.
import type { FirebaseWebConfig } from '@todayz/api-client/gen/model/firebaseWebConfig';
import type { OtpErrorKind } from './phone-otp';

/** What a Firebase failure means to the person: an OTP error kind, a bad number or a failed bot check. */
export type FirebasePhoneErrorKind = Exclude<OtpErrorKind, 'signinLocked' | 'methodChanged'> | 'invalidPhone' | 'captcha';

/** A Firebase failure, reduced to its kind; the SDK's own error (and its message) is not kept. */
export class FirebasePhoneError extends Error {
  constructor(readonly kind: FirebasePhoneErrorKind) {
    super(`firebase phone: ${kind}`);
    this.name = 'FirebasePhoneError';
  }
}

const KINDS: Record<string, FirebasePhoneErrorKind> = {
  'auth/invalid-verification-code': 'wrongCode',
  'auth/missing-verification-code': 'wrongCode',
  'auth/code-expired': 'expired',
  'auth/invalid-verification-id': 'expired',
  'auth/session-expired': 'expired',
  'auth/too-many-requests': 'rateLimited',
  'auth/quota-exceeded': 'rateLimited',
  'auth/invalid-phone-number': 'invalidPhone',
  'auth/missing-phone-number': 'invalidPhone',
  'auth/captcha-check-failed': 'captcha',
  'auth/invalid-app-credential': 'captcha',
  'auth/missing-app-credential': 'captcha',
  'auth/network-request-failed': 'network',
  'auth/user-disabled': 'blocked',
};

/** Maps a Firebase Auth error (its `code`) to the kind the screen shows. */
export function firebaseErrorKind(e: unknown): FirebasePhoneErrorKind {
  if (e instanceof FirebasePhoneError) return e.kind;
  const code = typeof e === 'object' && e !== null && 'code' in e ? (e as { code: unknown }).code : undefined;
  return (typeof code === 'string' && KINDS[code]) || 'generic';
}

/** One sent code. `confirm` resolves with the Firebase ID token; a wrong code may be retried. */
export interface FirebaseConfirmation {
  confirm(code: string): Promise<string>;
}

export interface FirebasePhone {
  /** Sends the code to an E.164 number, with SMS text in `languageCode` where Firebase has it. */
  send(e164: string, languageCode?: string): Promise<FirebaseConfirmation>;
}

type Sdk = { createFirebasePhone(config: FirebaseWebConfig): Promise<FirebasePhone> };

let sdk: Promise<Sdk> | null = null;

/** Loads the SDK chunk (once) and returns phone sign-in for this Firebase web config. */
export async function loadFirebasePhone(config: FirebaseWebConfig): Promise<FirebasePhone> {
  sdk ??= import('./firebase-sdk').catch((e: unknown) => {
    sdk = null;
    throw e;
  });
  let mod: Sdk;
  try {
    mod = await sdk;
  } catch {
    throw new FirebasePhoneError('network');
  }
  return mod.createFirebasePhone(config);
}
