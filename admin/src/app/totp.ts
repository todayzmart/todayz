import { ApiError, request } from '@todayz/api-client';

// TODO(W1-01b): replace these local types and calls with the generated client once
// `enrolTotp`, `verifyTotp` and `recoverTotp` are in the merged contract (backend/api/paths/auth.yaml).
// The shapes follow that file as it stands on the W1-01b branch. They need the OTP-step bearer, so
// they go through request() like every other call.

export interface TotpEnrol {
  /** Base32, for typing in by hand. Never logged, never stored. */
  secret: string;
  otpauthUri: string;
  /** 10 single-use codes, shown once. */
  recoveryCodes: string[];
}

export interface AccessGrant {
  accessToken: string;
  tokenType: 'Bearer';
  expiresAt: string;
}

export interface RecoverGrant extends AccessGrant {
  recoveryCodesLeft: number;
}

const JSON_HEADERS = { 'Content-Type': 'application/json' };

export function enrolTotp(): Promise<TotpEnrol> {
  return request<TotpEnrol>('/v1/auth/totp/enrol', { method: 'POST' });
}

export function verifyTotp(code: string, purpose: 'login' | 'step_up' = 'login'): Promise<AccessGrant> {
  return request<AccessGrant>('/v1/auth/totp/verify', {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify({ code, purpose }),
  });
}

export function recoverTotp(recoveryCode: string): Promise<RecoverGrant> {
  return request<RecoverGrant>('/v1/auth/totp/recover', {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify({ recoveryCode }),
  });
}

export type TotpErrorKind = 'wrong' | 'locked' | 'unavailable';

/** `attemptsLeft` is only set for a wrong code that the server counted. */
export function totpError(e: unknown): { kind: TotpErrorKind; attemptsLeft?: number } {
  if (e instanceof ApiError) {
    if (e.code === 'otp_locked') return { kind: 'locked' };
    if (e.code === 'code_invalid') {
      const left = e.problem.extensions?.['attemptsLeft'];
      return typeof left === 'number' ? { kind: 'wrong', attemptsLeft: left } : { kind: 'wrong' };
    }
  }
  return { kind: 'unavailable' };
}

/** "K7M2QXB4NP" -> "K7M2Q-XB4NP"; the API ignores hyphens, spaces and case, this only reads better. */
export function groupSecret(secret: string): string {
  return secret.replace(/\s+/g, '').replace(/(.{4})/g, '$1 ').trim();
}
