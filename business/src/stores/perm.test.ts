import { describe, expect, it } from 'vitest';
import {
  canEditPayout,
  canEditStaff,
  canManageStores,
  canMuteAsks,
  canOnboard,
  canRemoveStaff,
  canSeePayout,
  canSeeStaff,
  inviteRoles,
} from './perm';

describe('role limits (D-66, D-38, D-101)', () => {
  it('only owner and manager see staff and manage stores', () => {
    expect([canSeeStaff('owner'), canSeeStaff('manager'), canSeeStaff('cashier')]).toEqual([true, true, false]);
    expect([canManageStores('owner'), canManageStores('manager'), canManageStores('cashier')]).toEqual([true, true, false]);
  });

  it('the invite form offers the owner two roles, a manager only cashier, a cashier none', () => {
    expect(inviteRoles('owner')).toEqual(['manager', 'cashier']);
    expect(inviteRoles('manager')).toEqual(['cashier']);
    expect(inviteRoles('cashier')).toEqual([]);
  });

  it('only the owner edits staff, and never their own row', () => {
    expect(canEditStaff('owner', 'manager')).toBe(true);
    expect(canEditStaff('owner', 'owner')).toBe(false);
    expect(canEditStaff('manager', 'cashier')).toBe(false);
  });

  it('remove: the owner removes anyone but the owner; a manager removes cashiers only', () => {
    expect(canRemoveStaff('owner', 'manager')).toBe(true);
    expect(canRemoveStaff('owner', 'owner')).toBe(false);
    expect(canRemoveStaff('manager', 'cashier')).toBe(true);
    expect(canRemoveStaff('manager', 'manager')).toBe(false);
    expect(canRemoveStaff('manager', 'owner')).toBe(false);
    expect(canRemoveStaff('cashier', 'cashier')).toBe(false);
  });

  it('payout: the owner edits, a manager sees it masked, a cashier does not see it', () => {
    expect([canEditPayout('owner'), canEditPayout('manager'), canEditPayout('cashier')]).toEqual([true, false, false]);
    expect([canSeePayout('owner'), canSeePayout('manager'), canSeePayout('cashier')]).toEqual([true, true, false]);
  });

  it('asks can be muted by owner and manager; onboarding is the owner’s', () => {
    expect([canMuteAsks('owner'), canMuteAsks('manager'), canMuteAsks('cashier')]).toEqual([true, true, false]);
    expect([canOnboard('owner'), canOnboard('manager'), canOnboard('cashier')]).toEqual([true, false, false]);
  });
});
