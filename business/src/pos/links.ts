// Links the counter hands out: the bill QR the customer scans, the receipt link, and the share
// messages. The customer app lives on its own origin (`VITE_CUSTOMER_URL`, app.todayz.in).
// TODO(lead): the bill QR payload is assumed to be the URL `<customer origin>/scan/<token>` so that
// the phone camera opens C-17 directly (06 C-17 "also opens from the phone's camera via URL");
// the customer route and token format are not fixed in the plan yet.

export function customerOrigin(): string {
  const v: unknown = import.meta.env.VITE_CUSTOMER_URL;
  return (typeof v === 'string' && v ? v : 'https://app.todayz.localhost').replace(/\/+$/, '');
}

export function billQrUrl(token: string, origin: string = customerOrigin()): string {
  return `${origin}/scan/${encodeURIComponent(token)}`;
}

/** The public receipt page (C-24). The token is a 128-bit random value and carries no customer data. */
export function receiptUrl(token: string, origin: string = customerOrigin()): string {
  return `${origin}/r/${encodeURIComponent(token)}`;
}

/** Ten digits typed at the counter to the number WhatsApp and SMS want: 91XXXXXXXXXX. */
export function normalizeIndianPhone(input: string): string | null {
  const digits = input.replace(/\D/g, '');
  const ten = digits.length === 12 && digits.startsWith('91') ? digits.slice(2) : digits.length === 11 && digits.startsWith('0') ? digits.slice(1) : digits;
  return /^[6-9]\d{9}$/.test(ten) ? ten : null;
}

export function whatsappShareUrl(text: string, phone?: string | null): string {
  const to = phone ? normalizeIndianPhone(phone) : null;
  return `https://wa.me/${to ? `91${to}` : ''}?text=${encodeURIComponent(text)}`;
}

export function smsShareUrl(text: string, phone?: string | null): string {
  const to = phone ? normalizeIndianPhone(phone) : null;
  return `sms:${to ? `+91${to}` : ''}?body=${encodeURIComponent(text)}`;
}
