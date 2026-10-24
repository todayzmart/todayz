import { describe, expect, it } from 'vitest';
import { EMPTY_KYC, kycPayload, validateKyc } from './kyc';

describe('validateKyc', () => {
  it('accepts nothing at all (payout details are optional)', () => {
    expect(validateKyc(EMPTY_KYC)).toEqual({});
    expect(kycPayload(EMPTY_KYC)).toEqual({});
  });
  it('checks each format and upper-cases PAN, GSTIN and IFSC', () => {
    expect(validateKyc({ ...EMPTY_KYC, pan: 'abcde1234f' })).toEqual({});
    expect(validateKyc({ ...EMPTY_KYC, pan: 'ABCDE12345' }).pan).toBe('format');
    expect(validateKyc({ ...EMPTY_KYC, upiId: 'shop@upi' })).toEqual({});
    expect(validateKyc({ ...EMPTY_KYC, upiId: 'shop' }).upiId).toBe('format');
    expect(validateKyc({ ...EMPTY_KYC, bankAccount: '1234', bankIfsc: 'sbin0001234' }).bankAccount).toBe('format');
  });
  it('wants the bank account and IFSC together', () => {
    expect(validateKyc({ ...EMPTY_KYC, bankAccount: '123456789012' }).bankIfsc).toBe('pair');
    expect(validateKyc({ ...EMPTY_KYC, bankIfsc: 'SBIN0001234' }).bankAccount).toBe('pair');
    expect(validateKyc({ ...EMPTY_KYC, bankAccount: '123456789012', bankIfsc: 'SBIN0001234' })).toEqual({});
    // One half already saved: replacing only the other is fine.
    expect(validateKyc({ ...EMPTY_KYC, bankIfsc: 'SBIN0001234' }, { bank: true })).toEqual({});
  });
  it('wants the GSTIN to carry the PAN', () => {
    expect(validateKyc({ ...EMPTY_KYC, pan: 'ABCDE1234F', gstin: '29ABCDE1234F1Z5' })).toEqual({});
    expect(validateKyc({ ...EMPTY_KYC, pan: 'ABCDE1234F', gstin: '29ZZZZZ9999Z1Z5' }).gstin).toBe('pair');
  });
  it('sends only what was typed, cleaned', () => {
    expect(kycPayload({ ...EMPTY_KYC, pan: ' abcde1234f ', bankAccount: '1234 5678 9012' })).toEqual({ pan: 'ABCDE1234F', bankAccount: '123456789012' });
  });
});
