import { getMe } from '@todayz/api-client/gen/me/me';
import { useQuery } from '@tanstack/react-query';

/** The 7 admin roles (D-64). */
export const ROLES = ['field_agent', 'support', 'catalogue', 'analyst', 'finance', 'admin', 'super_admin'] as const;
export type Role = (typeof ROLES)[number];

const ADMINS: readonly Role[] = ['admin', 'super_admin'];
const SUPPORT_UP: readonly Role[] = ['support', ...ADMINS];

/**
 * Who opens each sidebar section, from the role table in 06-screens.md and the matrix in
 * 02-architecture.md §3.3. The API is the real gate; this only hides what would answer 403.
 * A section missing here is open to no one.
 */
export const SECTION_ROLES: Readonly<Record<string, readonly Role[]>> = {
  '': ['support', 'catalogue', 'analyst', 'finance', ...ADMINS],
  /** A-04 (admin.field.*): agents and admins. */
  field: ['field_agent', ...ADMINS],
  businesses: ['field_agent', ...SUPPORT_UP],
  stores: SUPPORT_UP,
  customers: SUPPORT_UP,
  orders: SUPPORT_UP,
  support: SUPPORT_UP,
  reviews: SUPPORT_UP,
  banners: SUPPORT_UP,
  /** A-13: support reads the queue only (D-64). */
  risk: SUPPORT_UP,
  credits: ['finance', ...ADMINS],
  /** A-10 (admin.referrals.view, risk.golden). */
  referrals: ADMINS,
  catalogue: ['catalogue', ...ADMINS],
  demand: ['field_agent', 'catalogue', 'analyst', ...ADMINS],
  /** A-15: an agent sees only their own row (D-64). */
  'field-agents': ['field_agent', ...ADMINS],
  payouts: ['finance', ...ADMINS],
  'admin-users': ['super_admin'],
  settings: ADMINS,
  audit: ADMINS,
  notifications: ADMINS,
  jobs: ['support', 'analyst', 'finance', ...ADMINS],
  system: ADMINS,
};

/** Buttons a screen shows only to the roles the API lets through (02 §3.3). */
export const ACTION_ROLES = {
  customerBlock: ADMINS,
  strikesReset: SUPPORT_UP,
  orderCancel: SUPPORT_UP,
  reviewModerate: SUPPORT_UP,
  bannerModerate: SUPPORT_UP,
  auditLog: ADMINS,
  businessView: SUPPORT_UP,
  businessVerify: ADMINS,
  kycReveal: ADMINS,
  storeReview: ADMINS,
  storeEdit: ADMINS,
  leads: ['field_agent', ...SUPPORT_UP],
  catalogueReview: ['catalogue', ...ADMINS],
  catalogueManage: ADMINS,
  /** A-11 hold, unhold, mark paid and export; the admin role reads only (06 A-11). */
  payoutManage: ['finance', 'super_admin'],
  /** A-09 adjustment request, approve and reject; the admin role cannot adjust credits (06 A-09). */
  creditAdjust: ['finance', 'super_admin'],
  /** A-13 clear and confirm (admin.risk.resolve). */
  riskResolve: ADMINS,
  /** A-10 reject (admin.referral.reject). */
  referralReject: ADMINS,
  /** A-15 every agent's row (`/v1/admin/field-agents`); an agent reads only `/field/me/stats`. */
  fieldAgentsAll: ADMINS,
} as const satisfies Record<string, readonly Role[]>;
export type Action = keyof typeof ACTION_ROLES;

export function canOpen(roles: readonly string[], section: string): boolean {
  const allowed = SECTION_ROLES[section];
  return !!allowed && allowed.some((r) => roles.includes(r));
}

export function can(roles: readonly string[], action: Action): boolean {
  return ACTION_ROLES[action].some((r) => roles.includes(r));
}

export const meKey = ['me'] as const;

/** The signed-in admin, whose `roles` drive the sidebar. Cleared with the cache on sign-out. */
export function useMe() {
  return useQuery({ queryKey: meKey, queryFn: () => getMe(), staleTime: 5 * 60_000 });
}

/** The caller's roles; empty while loading. */
export function useRoles(): readonly string[] {
  return useMe().data?.roles ?? NONE;
}

const NONE: readonly string[] = [];
