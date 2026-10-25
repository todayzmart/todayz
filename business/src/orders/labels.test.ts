import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { createIntl } from 'react-intl';
import { describe, expect, it } from 'vitest';
import { lineName, reasonLabel } from './labels';

const intl = createIntl({ locale: 'en-IN', messages: asMessages(en), onError: () => undefined });

describe('reasonLabel', () => {
  it('names the reasons the backend records for system expiries', () => {
    expect(reasonLabel(intl, 'not_accepted')).toBe('Not accepted in time');
    expect(reasonLabel(intl, 'no_show')).toBe('Not collected in time');
    expect(reasonLabel(intl, 'dispatch_timeout')).not.toBe(reasonLabel(intl, 'other'));
  });

  it('falls back to "other" for a reason it does not know', () => {
    expect(reasonLabel(intl, 'accept_timeout')).toBe(reasonLabel(intl, 'other'));
  });
});

describe('lineName', () => {
  it('adds the pack after the name', () => {
    expect(lineName({ name: 'Amul Butter', variant: null, packSize: 100, unit: 'g' })).toBe('Amul Butter · 100 g');
  });

  it('puts the variant between name and pack', () => {
    expect(lineName({ name: 'Tea', variant: 'Masala', packSize: 250, unit: 'g' })).toBe('Tea · Masala · 250 g');
  });

  it('does not repeat a pack the shop typed into the name', () => {
    expect(lineName({ name: 'Amul Butter 100 g', variant: null, packSize: 100, unit: 'g' })).toBe('Amul Butter 100 g');
    expect(lineName({ name: 'Atta 5kg', variant: null, packSize: 5, unit: 'kg' })).toBe('Atta 5kg');
  });
});
