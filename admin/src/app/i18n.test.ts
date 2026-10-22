import { describe, expect, it } from 'vitest';
import { initialLocale } from './i18n';

describe('initialLocale', () => {
  it('honours ?locale=pseudo in dev, test and the e2e build', () => {
    expect(initialLocale('?locale=pseudo', 'development')).toBe('en-XA');
    expect(initialLocale('?locale=pseudo', 'e2e')).toBe('en-XA');
    expect(initialLocale('', 'e2e')).toBe('en');
  });

  it('ignores ?locale=pseudo in a production build', () => {
    expect(initialLocale('?locale=pseudo', 'production')).toBe('en');
  });

  it('reads the current URL and Vite mode by default', () => {
    window.history.replaceState(null, '', '/?locale=pseudo');
    expect(import.meta.env.MODE).toBe('test');
    expect(initialLocale()).toBe('en-XA');
    window.history.replaceState(null, '', '/');
  });
});
