// A client-made ULID for `Idempotency-Key` (04 §1.6). Crockford base32: 10 characters of time, 16 of randomness.
const ALPHABET = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

export function newUlid(now: number = Date.now()): string {
  let t = now;
  let time = '';
  for (let i = 0; i < 10; i++) {
    time = ALPHABET.charAt(t % 32) + time;
    t = Math.floor(t / 32);
  }
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  let rand = '';
  for (const b of bytes) rand += ALPHABET.charAt(b % 32);
  return time + rand;
}
