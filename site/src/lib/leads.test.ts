// Lead form logic: phone normalisation to E.164, validation, and the request to POST /v1/public/leads.
// Test phone numbers come only from the reserved +91 90000 000xx block.
import { describe, expect, it, vi } from 'vitest';
import { LEAD_FIELDS, normalisePhone, submitLead, validateLead, type LeadRequest } from './leads.ts';

const PHONE = '+919000000012';

const request: LeadRequest = {
  name: 'Asha',
  shopName: 'Asha Stores',
  phone: PHONE,
  city: 'Bengaluru',
  source: 'site:home:en',
  turnstileToken: 'token',
};

describe('normalisePhone', () => {
  it.each([
    ['9000000012', PHONE],
    ['90000 00012', PHONE],
    ['+91 90000-00012', PHONE],
    ['+91 (90000) 00012', PHONE],
    ['919000000012', PHONE],
    ['91 90000 00012', PHONE],
    ['09000000012', PHONE],
    ['0091 90000 00012', PHONE],
    ['+91 09000000012', PHONE],
    ['9000000099', '+919000000099'],
  ])('accepts %s as %s', (raw, expected) => {
    expect(normalisePhone(raw)).toBe(expected);
  });

  it.each([
    '',
    '12345',
    '5000000012',
    '90000000123',
    'abcdefghij',
    '+1 9000000012',
    '+44 9000000012',
    '+91+9000000012',
    '0091 0091 9000000012',
    '+91 9000000012 ext 1',
  ])('rejects %s', (raw) => {
    expect(normalisePhone(raw)).toBeNull();
  });
});

describe('validateLead', () => {
  it('returns trimmed values and the phone in E.164', () => {
    const result = validateLead({ name: '  Asha ', shopName: ' Asha Stores', phone: '0091 90000 00012', city: 'Bengaluru ' });
    expect(result).toEqual({ ok: true, value: { name: 'Asha', shopName: 'Asha Stores', phone: PHONE, city: 'Bengaluru' } });
  });

  it('flags every invalid field by its camelCase name', () => {
    const result = validateLead({ name: 'A', shopName: '', phone: '123', city: ' ' });
    expect(result).toEqual({ ok: false, errors: { name: true, shopName: true, phone: true, city: true } });
  });

  it('enforces the length limits the inputs declare', () => {
    expect(validateLead({ name: 'a'.repeat(81), shopName: 'Asha Stores', phone: PHONE, city: 'Bengaluru' }).ok).toBe(false);
    expect(validateLead({ name: 'Asha', shopName: 'a'.repeat(121), phone: PHONE, city: 'Bengaluru' }).ok).toBe(false);
    expect(validateLead({ name: 'Asha', shopName: 'Asha Stores', phone: PHONE, city: 'a'.repeat(81) }).ok).toBe(false);
  });

  it('lists the fields in form order', () => {
    expect(LEAD_FIELDS).toEqual(['name', 'phone', 'shopName', 'city']);
  });
});

describe('submitLead', () => {
  const ok = () => Promise.resolve(new Response(null, { status: 201 }));

  it('posts camelCase JSON to /v1/public/leads on the API origin', async () => {
    const fetchImpl = vi.fn(ok);
    await expect(submitLead('https://api.todayz.in/', request, fetchImpl)).resolves.toEqual({ ok: true });
    expect(fetchImpl).toHaveBeenCalledOnce();
    const [url, init] = fetchImpl.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('https://api.todayz.in/v1/public/leads');
    expect(init.method).toBe('POST');
    expect(init.headers).toMatchObject({ 'content-type': 'application/json' });
    const body = JSON.parse(String(init.body)) as Record<string, unknown>;
    expect(body).toEqual({
      name: 'Asha',
      shopName: 'Asha Stores',
      phone: PHONE,
      city: 'Bengaluru',
      source: 'site:home:en',
      turnstileToken: 'token',
    });
    expect(Object.keys(body).some((k) => k.includes('_'))).toBe(false);
  });

  it.each([
    [400, 'rejected'],
    [422, 'rejected'],
    [403, 'rejected'],
    [429, 'rate_limited'],
    [500, 'server'],
    [503, 'server'],
  ] as const)('maps HTTP %i to %s', async (status, reason) => {
    const fetchImpl = vi.fn(() => Promise.resolve(new Response('{}', { status })));
    await expect(submitLead('https://api.todayz.in', request, fetchImpl)).resolves.toEqual({ ok: false, reason });
  });

  it('reports a network failure', async () => {
    const fetchImpl = vi.fn(() => Promise.reject(new TypeError('Failed to fetch')));
    await expect(submitLead('https://api.todayz.in', request, fetchImpl)).resolves.toEqual({ ok: false, reason: 'network' });
  });
});
