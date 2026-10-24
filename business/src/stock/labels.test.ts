import { createIntl } from 'react-intl';
import { describe, expect, it } from 'vitest';
import { dateText, whenText } from './labels';

const intl = (locale: string) => createIntl({ locale, messages: {}, onError: () => undefined });

describe('stock labels', () => {
  const NOW = Date.parse('2026-10-24T10:00:00.000Z');

  it('formats "12 min ago" for the html lang tag the shell passes (en-IN), not only the bare locale', () => {
    expect(whenText(intl('en-IN'), '2026-10-24T09:48:00.000Z', NOW)).toMatch(/12/);
    expect(() => whenText(intl('kn'), '2026-10-24T09:48:00.000Z', NOW)).not.toThrow();
  });

  it('writes an expiry as a calendar date with no clock arithmetic', () => {
    expect(dateText(intl('en-IN'), '2026-11-20')).toBe('20 Nov 2026');
    expect(dateText(intl('en-IN'), 'not-a-date')).toBe('not-a-date');
  });
});
