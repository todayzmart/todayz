import { describe, expect, it, vi } from 'vitest';
import { normalisePhone, submitLead, validateLead, type LeadRequest } from './leads.ts';

const request: LeadRequest = {
  name: 'Asha',
  shop_name: 'Asha Stores',
  phone: '9876543210',
  city: 'Bengaluru',
  source: 'site:home:en',
  turnstile_token: 'token',
};

describe('normalisePhone', () => {
  it.each([
    ['9876543210', '9876543210'],
    ['98765 43210', '9876543210'],
    ['+91 98765-43210', '9876543210'],
    ['919876543210', '9876543210'],
    ['09876543210', '9876543210'],
  ])('accepts %s', (raw, expected) => {
    expect(normalisePhone(raw)).toBe(expected);
  });

  it.each(['', '12345', '5876543210', '98765432101', 'abcdefghij'])('rejects %s', (raw) => {
    expect(normalisePhone(raw)).toBeNull();
  });
});

describe('validateLead', () => {
  it('returns trimmed, normalised values', () => {
    const result = validateLead({ name: ' Asha ', shop_name: 'Asha Stores', phone: '+91 98765 43210', city: ' Bengaluru ' });
    expect(result).toEqual({ ok: true, value: { name: 'Asha', shop_name: 'Asha Stores', phone: '9876543210', city: 'Bengaluru' } });
  });

  it('flags every bad field at once', () => {
    const result = validateLead({ name: '', shop_name: 'x', phone: '123', city: '' });
    expect(result).toEqual({ ok: false, errors: { name: true, shop_name: true, phone: true, city: true } });
  });
});

describe('submitLead', () => {
  it('posts JSON to /v1/public/leads and resolves ok on 2xx', async () => {
    const fetchMock = vi.fn(async () => new Response(null, { status: 201 }));
    const result = await submitLead('https://api.example.test/', request, fetchMock as unknown as typeof fetch);
    expect(result).toEqual({ ok: true });
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('https://api.example.test/v1/public/leads');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual(request);
  });

  it.each([
    [429, 'rate_limited'],
    [503, 'server'],
    [400, 'rejected'],
    [403, 'rejected'],
  ])('maps status %i to %s', async (status, reason) => {
    const fetchMock = vi.fn(async () => new Response(null, { status }));
    expect(await submitLead('https://api.example.test', request, fetchMock as unknown as typeof fetch)).toEqual({ ok: false, reason });
  });

  it('maps a thrown fetch to network', async () => {
    const fetchMock = vi.fn(async () => {
      throw new TypeError('offline');
    });
    expect(await submitLead('https://api.example.test', request, fetchMock as unknown as typeof fetch)).toEqual({ ok: false, reason: 'network' });
  });
});
