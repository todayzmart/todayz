import en from '@todayz/i18n/compiled/site/en.json';
import { describe, expect, it } from 'vitest';
import { formatMessage, t, type SiteKey } from './messages';

describe('site messages', () => {
  it('formats literals', () => {
    expect(t('site.hero.title')).toBe('Everything nearby, today.');
  });

  it('fills simple arguments', () => {
    expect(t('site.footer', { year: 2026 })).toBe('© 2026 TodayZ');
  });

  it('throws when an argument is missing', () => {
    expect(() => t('site.footer')).toThrow(/needs \{year\}/);
  });

  it('throws on a missing key', () => {
    expect(() => formatMessage(en, 'site.nope')).toThrow(/missing message/);
  });

  it('refuses ICU features it cannot format at build time', () => {
    const plural = { k: [{ type: 6, value: 'n' }] };
    expect(() => formatMessage(plural, 'k', { n: 1 })).toThrow(/ICU feature/);
  });

  it('every site message formats with a year', () => {
    for (const key of Object.keys(en) as SiteKey[]) {
      expect(t(key, { year: 2026 }).length).toBeGreaterThan(0);
    }
  });
});
