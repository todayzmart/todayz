import { describe, expect, it } from 'vitest';
import {
  ALL_DAY,
  copyFirstDay,
  defaultHours,
  hoursPayload,
  hoursValid,
  isAllDay,
  sameHours,
  validateDay,
  validateHours,
} from './hours';

describe('validateDay', () => {
  it('accepts a closed day, one range and split ranges', () => {
    expect(validateDay([])).toBeNull();
    expect(validateDay([{ open: '09:00', close: '21:00' }])).toBeNull();
    expect(validateDay([{ open: '09:00', close: '13:00' }, { open: '16:00', close: '21:00' }])).toBeNull();
  });
  it('accepts 24 hours and a 24:00 close', () => {
    expect(validateDay(ALL_DAY)).toBeNull();
    expect(isAllDay(ALL_DAY)).toBe(true);
  });
  it('treats a 00:00 close as midnight', () => {
    expect(validateDay([{ open: '18:00', close: '00:00' }])).toBeNull();
    expect(hoursPayload({ weekly: { fri: [{ open: '18:00', close: '00:00' }] }, holidays: [] }).weekly.fri).toEqual([
      { open: '18:00', close: '24:00' },
    ]);
  });
  it('refuses a range that closes before it opens, overlaps, or has bad times', () => {
    expect(validateDay([{ open: '21:00', close: '09:00' }])).toBe('order');
    expect(validateDay([{ open: '09:00', close: '09:00' }])).toBe('order');
    expect(validateDay([{ open: '09:00', close: '14:00' }, { open: '13:00', close: '18:00' }])).toBe('overlap');
    expect(validateDay([{ open: '9:00', close: '14:00' }])).toBe('time');
    expect(validateDay([{ open: '09:00', close: '25:00' }])).toBe('time');
  });
  it('allows four ranges a day and no more', () => {
    const r = (h: number) => ({ open: `${String(h).padStart(2, '0')}:00`, close: `${String(h).padStart(2, '0')}:30` });
    expect(validateDay([r(6), r(8), r(10), r(12)])).toBeNull();
    expect(validateDay([r(6), r(8), r(10), r(12), r(14)])).toBe('tooMany');
  });
});

describe('validateHours', () => {
  it('flags the broken day and the broken holiday by position', () => {
    const e = validateHours({
      weekly: { tue: [{ open: '10:00', close: '09:00' }] },
      holidays: [{ date: '2026-11-08', ranges: [] }, { date: '2026-11-08', ranges: [] }, { date: 'soon', ranges: [] }],
    });
    expect(e.tue).toBe('order');
    expect(e.holidays).toEqual({ 1: 'duplicate', 2: 'date' });
    expect(hoursValid(e)).toBe(false);
  });
  it('is valid for the defaults', () => {
    expect(hoursValid(validateHours(defaultHours()))).toBe(true);
  });
});

describe('payload', () => {
  it('leaves closed days out and sorts holidays', () => {
    const p = hoursPayload({
      weekly: { mon: [], tue: [{ open: '09:00', close: '21:00' }] },
      holidays: [{ date: '2026-12-25', ranges: [] }, { date: '2026-11-08', ranges: [] }],
    });
    expect(Object.keys(p.weekly)).toEqual(['tue']);
    expect(p.holidays.map((h) => h.date)).toEqual(['2026-11-08', '2026-12-25']);
  });
  it('copies one day to all', () => {
    const h = copyFirstDay({ weekly: { mon: [{ open: '08:00', close: '20:00' }] }, holidays: [] });
    expect(Object.keys(h.weekly)).toHaveLength(7);
    expect(sameHours(h, copyFirstDay(h))).toBe(true);
  });
});
