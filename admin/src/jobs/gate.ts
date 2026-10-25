import { can } from '../app/nav/access';

/**
 * Who may act on a kind (06 A-21): admin and super_admin; on a `money` kind super_admin only,
 * after step-up. `timer` kinds take no action from anyone. The API is the real gate.
 */
export function jobGate(roles: readonly string[], cls: string) {
  if (cls === 'timer') return { timer: true, may: false, moneyLocked: false };
  const acts = can(roles, 'jobAct');
  const moneyLocked = cls === 'money' && acts && !can(roles, 'jobMoneyAct');
  return { timer: false, may: acts && !moneyLocked, moneyLocked };
}
