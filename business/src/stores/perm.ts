import type { StaffInviteRole } from '@todayz/api-client/gen/model/staffInviteRole';
import type { StaffRole } from '@todayz/api-client/gen/model/staffRole';

/**
 * What each role sees in B-02, B-13 and B-14 (06-screens "Role limits in the UI", D-06, D-38, D-65,
 * D-66). The API is the guard; the UI hides what a role cannot do, so a person never meets a 403 in
 * a normal flow. Role is per business (`BusinessSummary.role`).
 */

/** B-13 is for owner and manager; cashiers do not see staff. */
export const canSeeStaff = (role: StaffRole): boolean => role === 'owner' || role === 'manager';

/** Add a store, send a draft store for review, edit its profile, hours and settings. */
export const canManageStores = (role: StaffRole): boolean => role === 'owner' || role === 'manager';

/** The roles the invite form offers: the owner invites managers and cashiers, a manager only cashiers (D-66). */
export function inviteRoles(role: StaffRole): readonly StaffInviteRole[] {
  if (role === 'owner') return ['manager', 'cashier'];
  if (role === 'manager') return ['cashier'];
  return [];
}

/** Change a person's role, stores or delivery flag: owner only, and never the owner's own row (D-66). */
export const canEditStaff = (actor: StaffRole, target: StaffRole): boolean => actor === 'owner' && target !== 'owner';

/** The owner removes anyone but themselves; a manager removes cashiers only (D-66). */
export function canRemoveStaff(actor: StaffRole, target: StaffRole): boolean {
  if (actor === 'owner') return target !== 'owner';
  if (actor === 'manager') return target === 'cashier';
  return false;
}

/** Payout and tax details: the owner edits; a manager sees them masked (D-38, D-44); a cashier sees nothing. */
export const canEditPayout = (role: StaffRole): boolean => role === 'owner';
export const canSeePayout = (role: StaffRole): boolean => role === 'owner' || role === 'manager';

/** "Take asks from customers" needs `asks.manage`: owner and manager (D-101). */
export const canMuteAsks = (role: StaffRole): boolean => role === 'owner' || role === 'manager';

/** Business details, documents and the submit for verification are the owner's. */
export const canOnboard = (role: StaffRole): boolean => role === 'owner';
