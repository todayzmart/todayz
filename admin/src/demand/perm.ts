/**
 * A-23 roles (06 A-23, D-101). Gaps is `admin.demand_gaps.view` (the section's roles, in
 * app/nav/access.ts); the Site strip tab and Approve are `admin.marquee.approve`. The API is the guard.
 */
const MARQUEE: readonly string[] = ['catalogue', 'admin', 'super_admin'];

export const canApproveStrip = (roles: readonly string[]): boolean => roles.some((r) => MARQUEE.includes(r));
