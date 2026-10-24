import { describe, expect, it } from 'vitest';
import { lineName } from './labels';

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
