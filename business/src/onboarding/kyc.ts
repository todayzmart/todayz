import type { KycUpdate } from '@todayz/api-client/gen/model/kycUpdate';

/** What the person typed. A field left empty is left unchanged: the API never returns the full values. */
export interface KycValues {
  legalName: string;
  pan: string;
  gstin: string;
  upiId: string;
  bankAccount: string;
  bankIfsc: string;
}

export const EMPTY_KYC: KycValues = { legalName: '', pan: '', gstin: '', upiId: '', bankAccount: '', bankIfsc: '' };

// The API's patterns (`KycUpdate`); the server also checks the GSTIN checksum and that it carries the PAN.
const PAN = /^[A-Z]{5}[0-9]{4}[A-Z]$/;
const GSTIN = /^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$/;
const UPI = /^[a-zA-Z0-9._-]{2,256}@[a-zA-Z]{2,64}$/;
const ACCOUNT = /^[0-9]{9,18}$/;
const IFSC = /^[A-Z]{4}0[A-Z0-9]{6}$/;

export type KycErrors = Partial<Record<keyof KycValues, 'format' | 'pair'>>;

export const clean = (v: KycValues): KycValues => ({
  legalName: v.legalName.trim(),
  pan: v.pan.trim().toUpperCase(),
  gstin: v.gstin.trim().toUpperCase(),
  upiId: v.upiId.trim(),
  bankAccount: v.bankAccount.replace(/\s/g, ''),
  bankIfsc: v.bankIfsc.trim().toUpperCase(),
});

/** `have` says which bank half is already saved, so replacing only one of the two is caught here, not by the server. */
export function validateKyc(raw: KycValues, have: { bank: boolean } = { bank: false }): KycErrors {
  const v = clean(raw);
  const e: KycErrors = {};
  if (v.legalName.length > 120) e.legalName = 'format';
  if (v.pan && !PAN.test(v.pan)) e.pan = 'format';
  if (v.gstin && !GSTIN.test(v.gstin)) e.gstin = 'format';
  else if (v.gstin && v.pan && v.gstin.slice(2, 12) !== v.pan) e.gstin = 'pair';
  if (v.upiId && !UPI.test(v.upiId)) e.upiId = 'format';
  if (v.bankAccount && !ACCOUNT.test(v.bankAccount)) e.bankAccount = 'format';
  if (v.bankIfsc && !IFSC.test(v.bankIfsc)) e.bankIfsc = 'format';
  if (!have.bank) {
    if (v.bankAccount && !v.bankIfsc) e.bankIfsc = 'pair';
    if (v.bankIfsc && !v.bankAccount) e.bankAccount = 'pair';
  }
  return e;
}

/** Only the fields that were filled in. An empty object means there is nothing to save. */
export function kycPayload(raw: KycValues): KycUpdate {
  const v = clean(raw);
  const out: KycUpdate = {};
  for (const k of ['legalName', 'pan', 'gstin', 'upiId', 'bankAccount', 'bankIfsc'] as const) {
    if (v[k]) out[k] = v[k];
  }
  return out;
}
