/**
 * TODO(lead): the plan names "call support" on B-15 but fixes no number. It is read from the build
 * (`VITE_SUPPORT_PHONE`, digits with an optional leading +); unset, the card offers a ticket instead.
 */
export function supportPhone(): string | null {
  const v: unknown = import.meta.env.VITE_SUPPORT_PHONE;
  return typeof v === 'string' && /^\+?[0-9]{8,15}$/.test(v.trim()) ? v.trim() : null;
}
