import { ApiError } from '@todayz/api-client';

import {
  enrolTotp as apiEnrolTotp,
  recoverTotp as apiRecoverTotp,
  verifyTotp as apiVerifyTotp,
} from '@todayz/api-client/gen/auth/auth';
import type { AccessTokenResponse } from '@todayz/api-client/gen/model/accessTokenResponse';
import type { TotpEnrolResponse } from '@todayz/api-client/gen/model/totpEnrolResponse';
import type { TotpRecoverResponse } from '@todayz/api-client/gen/model/totpRecoverResponse';

// The admin TOTP calls (backend/api/paths/auth.yaml). They need the OTP-step bearer, so they go through
// request() like every other call; request() sends the cookie to every /v1/auth/ route, which stores the
// refresh cookie a login verify or a recovery rotates.

export function enrolTotp(): Promise<TotpEnrolResponse> {
  return apiEnrolTotp();
}

export function verifyTotp(code: string, purpose: 'login' | 'step_up' = 'login'): Promise<AccessTokenResponse> {
  return apiVerifyTotp({ code, purpose });
}

export function recoverTotp(recoveryCode: string): Promise<TotpRecoverResponse> {
  return apiRecoverTotp({ recoveryCode });
}

export type TotpErrorKind = 'wrong' | 'locked' | 'stepUp' | 'unavailable';

/** `attemptsLeft` is only set for a wrong code that the server counted. */
export function totpError(e: unknown): { kind: TotpErrorKind; attemptsLeft?: number } {
  if (e instanceof ApiError) {
    if (e.code === 'otp_locked') return { kind: 'locked' };
    // 403 from the admin API: the action needs a fresh `purpose: step_up` confirmation (not built yet).
    if (e.code === 'step_up_required') return { kind: 'stepUp' };
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
