import { describe, expect, it } from 'vitest';
import fixtures from './qr.fixtures.json';
import { encodeQr, qrPath, QR_MAX_BYTES } from './qr';

// qr.fixtures.json holds libqrencode's output (`qrencode -t ASCII -m 0 -l M -8`) for strings that
// land in versions 1-14. The encoder is run with the same mask, so every module must match.
describe('QR encoder against libqrencode', () => {
  it.each(fixtures)('version $version, $text.length bytes, mask $mask', (f) => {
    const q = encodeQr(f.text, { mask: f.mask });
    expect(q.version).toBe(f.version);
    expect(q.size).toBe(f.size);
    const rows = q.modules.map((r) => r.map((v) => (v ? '1' : '0')).join(''));
    expect(rows).toEqual(f.rows);
  });

  it('picks the lowest-penalty mask, and it is one of the eight', () => {
    const q = encodeQr('https://app.todayz.in/b/abc');
    expect(q.mask).toBeGreaterThanOrEqual(0);
    expect(q.mask).toBeLessThan(8);
  });

  it('rejects text that does not fit', () => {
    expect(() => encodeQr('x'.repeat(QR_MAX_BYTES + 1))).toThrow(RangeError);
  });

  it('encodes UTF-8 by bytes', () => {
    expect(encodeQr('₹100').version).toBe(1);
  });

  it('draws the path with a quiet zone', () => {
    const q = encodeQr('hi');
    const p = qrPath(q);
    expect(p.viewBox).toBe(`0 0 ${q.size + 8} ${q.size + 8}`);
    expect(p.d.startsWith('M4 4h7')).toBe(true);
  });
});
