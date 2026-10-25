/** An Indian mobile in the forms an operator types: 98765 43210, +91 98765 43210, 919876543210. */
export function mobileDigits(raw: string): string | null {
  const d = raw.replace(/[\s+-]/g, '');
  if (/^[6-9]\d{9}$/.test(d)) return d;
  if (/^91[6-9]\d{9}$/.test(d)) return d.slice(2);
  return null;
}
