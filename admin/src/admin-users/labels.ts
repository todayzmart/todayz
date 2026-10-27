import type { AdminRole } from '@todayz/api-client/gen/model/adminRole';
import { ApiError } from '@todayz/api-client';
import { useCallback, useMemo } from 'react';
import { useIntl } from 'react-intl';
import { ROLES } from '../app/nav/access';
import { useProblemText } from '../app/problems';

const ROLE_LABEL: Record<AdminRole, string> = {
  field_agent: 'shell.role.field_agent',
  support: 'shell.role.support',
  catalogue: 'shell.role.catalogue',
  analyst: 'shell.role.analyst',
  finance: 'shell.role.finance',
  admin: 'shell.role.admin',
  super_admin: 'shell.role.super_admin',
};

/** The 7 roles in the order the role table lists them. */
export const ADMIN_ROLES = ROLES as readonly AdminRole[];

/** A number the API reads as an Indian mobile: 10 digits, or 12 starting with 91 (spaces and `+` ignored). */
export function phoneOk(v: string): boolean {
  const d = v.replace(/[\s+]/g, '');
  return /^\d{10}$/.test(d) || /^91\d{10}$/.test(d);
}

export function useRoleLabel() {
  const intl = useIntl();
  return useMemo(() => {
    const one = (r: AdminRole) => intl.formatMessage({ id: ROLE_LABEL[r] });
    return { one, list: (rs: readonly AdminRole[]) => rs.map(one).join(', ') };
  }, [intl]);
}

/**
 * One sentence for a failed A-18 change. A step-up request is answered by the dialog, so it has none;
 * the conflict and refusal codes get this screen's own words.
 */
export function useAccountError() {
  const intl = useIntl();
  const problemText = useProblemText();
  return useCallback(
    (err: unknown, during: 'invite' | 'change' | 'unlock'): string | null => {
      if (!err) return null;
      if (err instanceof ApiError) {
        if (err.code === 'step_up_required') return null;
        if (err.code === 'conflict') return intl.formatMessage({ id: during === 'invite' ? 'adminUsers.error.exists' : 'adminUsers.error.stale' });
        if (err.code === 'invalid_transition') {
          return intl.formatMessage({ id: during === 'unlock' ? 'adminUsers.error.notLocked' : 'adminUsers.error.refused' });
        }
      }
      return problemText(err);
    },
    [intl, problemText],
  );
}
