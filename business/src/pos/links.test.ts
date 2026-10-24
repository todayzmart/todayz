import { describe, expect, it } from 'vitest';
import { billQrUrl, normalizeIndianPhone, receiptUrl, smsShareUrl, whatsappShareUrl } from './links';

describe('links', () => {
  it('builds the bill QR and receipt urls on the customer origin', () => {
    expect(billQrUrl('abc.def', 'https://app.example')).toBe('https://app.example/scan/abc.def');
    expect(receiptUrl('t0k3n', 'https://app.example')).toBe('https://app.example/r/t0k3n');
  });

  it('normalises Indian numbers and refuses others', () => {
    expect(normalizeIndianPhone('98450 14521')).toBe('9845014521');
    expect(normalizeIndianPhone('+91 98450 14521')).toBe('9845014521');
    expect(normalizeIndianPhone('09845014521')).toBe('9845014521');
    expect(normalizeIndianPhone('12345')).toBeNull();
    expect(normalizeIndianPhone('5845014521')).toBeNull();
  });

  it('shares to the customer when the number is valid, to a chooser otherwise', () => {
    expect(whatsappShareUrl('hi there', '9845014521')).toBe('https://wa.me/919845014521?text=hi%20there');
    expect(whatsappShareUrl('hi')).toBe('https://wa.me/?text=hi');
    expect(smsShareUrl('hi', '9845014521')).toBe('sms:+919845014521?body=hi');
    expect(smsShareUrl('hi', 'nope')).toBe('sms:?body=hi');
  });
});
