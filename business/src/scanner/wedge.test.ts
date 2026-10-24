import { describe, expect, it } from 'vitest';
import { createWedgeDetector, looksLikeBarcode } from './wedge';

function type(det: ReturnType<typeof createWedgeDetector>, text: string, gap: number, start = 1000): { code: string | null; end: number } {
  let t = start;
  for (const ch of text) {
    det.key({ key: ch }, t);
    t += gap;
  }
  const code = det.key({ key: 'Enter' }, t);
  return { code, end: t };
}

describe('wedge scanner detection', () => {
  it('accepts a fast run that ends in Enter', () => {
    expect(type(createWedgeDetector(), '8901058000177', 8).code).toBe('8901058000177');
  });

  it('ignores a person typing at normal speed', () => {
    expect(type(createWedgeDetector(), '8901058000177', 140).code).toBeNull();
  });

  it('ignores a run that is too short', () => {
    expect(type(createWedgeDetector(), '123', 5).code).toBeNull();
  });

  it('drops keys typed before a pause and keeps only the fast run', () => {
    const det = createWedgeDetector();
    det.key({ key: 'x' }, 100);
    det.key({ key: 'y' }, 900);
    expect(type(det, '8901058000177', 5, 2000).code).toBe('8901058000177');
  });

  it('does not count modifier shortcuts', () => {
    const det = createWedgeDetector();
    det.key({ key: 'a', ctrlKey: true }, 10);
    expect(type(det, '12345678', 5, 20).code).toBe('12345678');
  });

  it('handles two scans in a row', () => {
    const det = createWedgeDetector();
    const a = type(det, '8901058000177', 6);
    const b = type(det, '8901234567890', 6, a.end + 500);
    expect(b.code).toBe('8901234567890');
  });

  it('knows a barcode from a word', () => {
    expect(looksLikeBarcode('8901058000177')).toBe(true);
    expect(looksLikeBarcode(' 8901058000177 ')).toBe(true);
    expect(looksLikeBarcode('atta')).toBe(false);
    expect(looksLikeBarcode('12')).toBe(false);
  });
});
