// A small QR Code encoder for the bill QR (B-05). The plan names no QR library and the lockfile has
// none, so this is written here: byte mode, error correction level M, versions 1-14 (up to 362
// bytes), which is far more than a bill token needs. `qr.test.ts` compares the output with
// libqrencode's for strings of every version (fixtures in `qr.fixtures.json`).
// Follows ISO/IEC 18004: Reed-Solomon over GF(256) (0x11D), the eight masks, and the four-rule penalty.

export interface QrMatrix {
  version: number;
  mask: number;
  size: number;
  /** modules[row][col], true = dark. */
  modules: boolean[][];
}

/** Level M: [EC codewords per block, [blocks, data codewords per block][]], versions 1-14. */
const BLOCKS_M: Record<number, [number, [number, number][]]> = {
  1: [10, [[1, 16]]],
  2: [16, [[1, 28]]],
  3: [26, [[1, 44]]],
  4: [18, [[2, 32]]],
  5: [24, [[2, 43]]],
  6: [16, [[4, 27]]],
  7: [18, [[4, 31]]],
  8: [22, [[2, 38], [2, 39]]],
  9: [22, [[3, 36], [2, 37]]],
  10: [26, [[4, 43], [1, 44]]],
  11: [30, [[1, 50], [4, 51]]],
  12: [22, [[6, 36], [2, 37]]],
  13: [22, [[8, 37], [1, 38]]],
  14: [24, [[4, 40], [5, 41]]],
};

const ALIGN: Record<number, number[]> = {
  1: [],
  2: [6, 18],
  3: [6, 22],
  4: [6, 26],
  5: [6, 30],
  6: [6, 34],
  7: [6, 22, 38],
  8: [6, 24, 42],
  9: [6, 26, 46],
  10: [6, 28, 50],
  11: [6, 30, 54],
  12: [6, 32, 58],
  13: [6, 34, 62],
  14: [6, 26, 46, 66],
};

export const QR_MAX_BYTES = 362;

const EXP: number[] = [];
const LOG: number[] = [];
(() => {
  let x = 1;
  for (let i = 0; i < 255; i += 1) {
    EXP[i] = x;
    LOG[x] = i;
    x <<= 1;
    if (x & 0x100) x ^= 0x11d;
  }
  for (let i = 255; i < 512; i += 1) EXP[i] = EXP[i - 255] ?? 0;
})();

function gfMul(a: number, b: number): number {
  return a === 0 || b === 0 ? 0 : (EXP[((LOG[a] ?? 0) + (LOG[b] ?? 0)) % 255] ?? 0);
}

function generator(degree: number): number[] {
  let poly = [1];
  for (let i = 0; i < degree; i += 1) {
    const next = new Array<number>(poly.length + 1).fill(0);
    poly.forEach((c, j) => {
      next[j] = (next[j] ?? 0) ^ c;
      next[j + 1] = (next[j + 1] ?? 0) ^ gfMul(c, EXP[i] ?? 0);
    });
    poly = next;
  }
  return poly;
}

function reedSolomon(data: number[], ecLen: number): number[] {
  const gen = generator(ecLen);
  const rem = new Array<number>(ecLen).fill(0);
  for (const d of data) {
    const factor = d ^ (rem.shift() ?? 0);
    rem.push(0);
    for (let i = 0; i < ecLen; i += 1) rem[i] = (rem[i] ?? 0) ^ gfMul(gen[i + 1] ?? 0, factor);
  }
  return rem;
}

function dataCapacity(version: number): number {
  const spec = BLOCKS_M[version];
  return spec ? spec[1].reduce((n, [count, len]) => n + count * len, 0) : 0;
}

function pickVersion(byteLen: number): number {
  for (let v = 1; v <= 14; v += 1) {
    const countBits = v < 10 ? 8 : 16;
    if (4 + countBits + byteLen * 8 <= dataCapacity(v) * 8) return v;
  }
  throw new RangeError('qr: text too long');
}

function buildCodewords(bytes: Uint8Array, version: number): number[] {
  const bits: number[] = [];
  const put = (value: number, len: number) => {
    for (let i = len - 1; i >= 0; i -= 1) bits.push((value >>> i) & 1);
  };
  put(0b0100, 4);
  put(bytes.length, version < 10 ? 8 : 16);
  for (const b of bytes) put(b, 8);
  const capBits = dataCapacity(version) * 8;
  put(0, Math.min(4, capBits - bits.length));
  while (bits.length % 8 !== 0) bits.push(0);
  const out: number[] = [];
  for (let i = 0; i < bits.length; i += 8) out.push(bits.slice(i, i + 8).reduce((a, b) => (a << 1) | b, 0));
  for (let pad = 0; out.length < dataCapacity(version); pad += 1) out.push(pad % 2 === 0 ? 0xec : 0x11);
  return out;
}

function interleave(data: number[], version: number): number[] {
  const spec = BLOCKS_M[version];
  if (!spec) throw new RangeError('qr: unsupported version');
  const [ecLen, groups] = spec;
  const blocks: { data: number[]; ec: number[] }[] = [];
  let offset = 0;
  for (const [count, len] of groups) {
    for (let i = 0; i < count; i += 1) {
      const d = data.slice(offset, offset + len);
      offset += len;
      blocks.push({ data: d, ec: reedSolomon(d, ecLen) });
    }
  }
  const out: number[] = [];
  const maxLen = Math.max(...blocks.map((b) => b.data.length));
  for (let i = 0; i < maxLen; i += 1) for (const b of blocks) if (i < b.data.length) out.push(b.data[i] ?? 0);
  for (let i = 0; i < ecLen; i += 1) for (const b of blocks) out.push(b.ec[i] ?? 0);
  return out;
}

function bch(value: number, poly: number, polyBits: number): number {
  const digits = (n: number) => 32 - Math.clz32(n);
  let d = value;
  while (digits(d) - polyBits >= 0 && d !== 0) d ^= poly << (digits(d) - polyBits);
  return d;
}

function formatBits(mask: number): number {
  // Level M = 00, so the five data bits are 00 followed by the mask number.
  const data = mask;
  return ((data << 10) | bch(data << 10, 0x537, 11)) ^ 0x5412;
}

function versionBits(version: number): number {
  return (version << 12) | bch(version << 12, 0x1f25, 13);
}

function maskHit(mask: number, i: number, j: number): boolean {
  switch (mask) {
    case 0: return (i + j) % 2 === 0;
    case 1: return i % 2 === 0;
    case 2: return j % 3 === 0;
    case 3: return (i + j) % 3 === 0;
    case 4: return (Math.floor(i / 2) + Math.floor(j / 3)) % 2 === 0;
    case 5: return ((i * j) % 2) + ((i * j) % 3) === 0;
    case 6: return (((i * j) % 2) + ((i * j) % 3)) % 2 === 0;
    default: return (((i * j) % 3) + ((i + j) % 2)) % 2 === 0;
  }
}

/** The matrix with all function patterns; `null` marks a data module still to be placed. */
function baseMatrix(version: number): (boolean | null)[][] {
  const size = version * 4 + 17;
  const m: (boolean | null)[][] = Array.from({ length: size }, () => new Array<boolean | null>(size).fill(null));
  const set = (r: number, c: number, v: boolean) => {
    const row = m[r];
    if (row && c >= 0 && c < size) row[c] = v;
  };
  const finder = (r0: number, c0: number) => {
    for (let r = -1; r <= 7; r += 1) {
      for (let c = -1; c <= 7; c += 1) {
        const inside = r >= 0 && r <= 6 && c >= 0 && c <= 6;
        const dark = inside && (r === 0 || r === 6 || c === 0 || c === 6 || (r >= 2 && r <= 4 && c >= 2 && c <= 4));
        set(r0 + r, c0 + c, dark);
      }
    }
  };
  finder(0, 0);
  finder(0, size - 7);
  finder(size - 7, 0);
  for (let i = 8; i < size - 8; i += 1) {
    set(6, i, i % 2 === 0);
    set(i, 6, i % 2 === 0);
  }
  const pos = ALIGN[version] ?? [];
  const last = pos.length - 1;
  pos.forEach((r, ri) => {
    pos.forEach((c, ci) => {
      if ((ri === 0 && ci === 0) || (ri === 0 && ci === last) || (ri === last && ci === 0)) return;
      for (let dr = -2; dr <= 2; dr += 1) {
        for (let dc = -2; dc <= 2; dc += 1) {
          set(r + dr, c + dc, Math.max(Math.abs(dr), Math.abs(dc)) !== 1);
        }
      }
    });
  });
  // Reserve the format and version areas (real values are written after the mask is chosen).
  for (let i = 0; i < 9; i += 1) {
    if (i === 6) continue; // the timing pattern runs through here
    set(8, i, false);
    set(i, 8, false);
  }
  for (let i = 0; i < 8; i += 1) {
    set(8, size - 1 - i, false);
    set(size - 1 - i, 8, false);
  }
  if (version >= 7) {
    for (let i = 0; i < 18; i += 1) {
      const a = Math.floor(i / 3);
      const b = (i % 3) + size - 11;
      set(a, b, false);
      set(b, a, false);
    }
  }
  return m;
}

function placeInfo(m: boolean[][], version: number, mask: number) {
  const size = m.length;
  const set = (r: number, c: number, v: boolean) => {
    const row = m[r];
    if (row) row[c] = v;
  };
  const fmt = formatBits(mask);
  for (let i = 0; i < 15; i += 1) {
    const bit = ((fmt >> i) & 1) === 1;
    if (i < 6) set(i, 8, bit);
    else if (i < 8) set(i + 1, 8, bit);
    else set(size - 15 + i, 8, bit);
    if (i < 8) set(8, size - i - 1, bit);
    else if (i < 9) set(8, 15 - i - 1 + 1, bit);
    else set(8, 15 - i - 1, bit);
  }
  set(size - 8, 8, true);
  if (version >= 7) {
    const v = versionBits(version);
    for (let i = 0; i < 18; i += 1) {
      const bit = ((v >> i) & 1) === 1;
      set(Math.floor(i / 3), (i % 3) + size - 11, bit);
      set((i % 3) + size - 11, Math.floor(i / 3), bit);
    }
  }
}

function withData(base: (boolean | null)[][], codewords: number[], version: number, mask: number): boolean[][] {
  const size = base.length;
  const m = base.map((row) => row.slice());
  let byteIndex = 0;
  let bitIndex = 7;
  let row = size - 1;
  let inc = -1;
  for (let col = size - 1; col > 0; col -= 2) {
    if (col === 6) col -= 1;
    for (;;) {
      for (let c = 0; c < 2; c += 1) {
        const line = m[row];
        if (line && line[col - c] === null) {
          let dark = false;
          if (byteIndex < codewords.length) dark = (((codewords[byteIndex] ?? 0) >>> bitIndex) & 1) === 1;
          if (maskHit(mask, row, col - c)) dark = !dark;
          line[col - c] = dark;
          bitIndex -= 1;
          if (bitIndex === -1) {
            byteIndex += 1;
            bitIndex = 7;
          }
        }
      }
      row += inc;
      if (row < 0 || row >= size) {
        row -= inc;
        inc = -inc;
        break;
      }
    }
  }
  const done = m.map((r) => r.map((v) => v === true));
  placeInfo(done, version, mask);
  return done;
}

function penalty(m: boolean[][]): number {
  const size = m.length;
  const at = (r: number, c: number) => m[r]?.[c] === true;
  let score = 0;
  for (let pass = 0; pass < 2; pass += 1) {
    for (let a = 0; a < size; a += 1) {
      let run = 1;
      for (let b = 1; b < size; b += 1) {
        const same = pass === 0 ? at(a, b) === at(a, b - 1) : at(b, a) === at(b - 1, a);
        if (same) run += 1;
        else {
          if (run >= 5) score += 3 + (run - 5);
          run = 1;
        }
      }
      if (run >= 5) score += 3 + (run - 5);
    }
  }
  for (let r = 0; r < size - 1; r += 1) {
    for (let c = 0; c < size - 1; c += 1) {
      const v = at(r, c);
      if (v === at(r, c + 1) && v === at(r + 1, c) && v === at(r + 1, c + 1)) score += 3;
    }
  }
  const finderLike = (get: (k: number) => boolean, start: number) => {
    const pat = [true, false, true, true, true, false, true];
    for (let k = 0; k < 7; k += 1) if (get(start + k) !== pat[k]) return false;
    const light = (from: number, to: number) => {
      for (let k = from; k < to; k += 1) if (k >= 0 && k < size && get(k)) return false;
      return true;
    };
    return light(start - 4, start) || light(start + 7, start + 11);
  };
  for (let a = 0; a < size; a += 1) {
    for (let b = 0; b + 7 <= size; b += 1) {
      if (finderLike((k) => at(a, k), b)) score += 40;
      if (finderLike((k) => at(k, a), b)) score += 40;
    }
  }
  let dark = 0;
  for (let r = 0; r < size; r += 1) for (let c = 0; c < size; c += 1) if (at(r, c)) dark += 1;
  const pct = (dark * 100) / (size * size);
  score += Math.floor(Math.abs(pct - 50) / 5) * 10;
  return score;
}

/** Encodes `text` (UTF-8) at level M. `opts.mask` forces a mask (tests); otherwise the lowest penalty wins. */
export function encodeQr(text: string, opts: { mask?: number } = {}): QrMatrix {
  const bytes = new TextEncoder().encode(text);
  if (bytes.length > QR_MAX_BYTES) throw new RangeError('qr: text too long');
  const version = pickVersion(bytes.length);
  const codewords = interleave(buildCodewords(bytes, version), version);
  const base = baseMatrix(version);
  let best: { mask: number; modules: boolean[][]; score: number } | null = null;
  const masks = opts.mask === undefined ? [0, 1, 2, 3, 4, 5, 6, 7] : [opts.mask];
  for (const mask of masks) {
    const modules = withData(base, codewords, version, mask);
    const score = penalty(modules);
    if (!best || score < best.score) best = { mask, modules, score };
  }
  if (!best) throw new RangeError('qr: bad mask');
  return { version, mask: best.mask, size: best.modules.length, modules: best.modules };
}

/** An SVG path (`M x y h w v 1 h -w z` per run of dark modules) in module units, with a 4-module quiet zone. */
export function qrPath(q: QrMatrix, quiet = 4): { d: string; viewBox: string } {
  const parts: string[] = [];
  q.modules.forEach((row, r) => {
    let c = 0;
    while (c < row.length) {
      if (!row[c]) {
        c += 1;
        continue;
      }
      let end = c;
      while (end < row.length && row[end]) end += 1;
      parts.push(`M${c + quiet} ${r + quiet}h${end - c}v1h${c - end}z`);
      c = end;
    }
  });
  const full = q.size + quiet * 2;
  return { d: parts.join(''), viewBox: `0 0 ${full} ${full}` };
}
