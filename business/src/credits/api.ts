/**
 * Credit account, business referrals and the standee PDF: endpoints in 04-api.md (W2-01, W2-03,
 * W2-10) that are not in the generated client yet.
 *
 * TODO(merge): switch to the generated client when `getCreditAccount` and `getQrStandee` land
 * (`business-credits.yaml`, `standee.yaml`), and delete this file. The request and answer shapes below
 * are this screen's assumption of those operations. `getBusinessReferrals` is generated now
 * (`@todayz/api-client/gen/referrals/referrals`) and was swapped in by W2-10 finish.
 *
 * Assumed endpoints (`{B}` = `/v1/biz/businesses/{bid}`, `{S}` = `{B}/stores/{sid}`):
 * - `GET {B}/credit-account?cursor=&limit=`
 * - `GET {S}/qr-standee.pdf` (Blob)
 */
import { request } from '@todayz/api-client';

/**
 * What a line is (01 §7, §8, §9). Spent at your shop and referral rewards add credits; cashback you
 * gave, reversing a POS sale and payouts take them away; an admin adjustment carries its own sign.
 */
export type CreditLineKind = 'spent' | 'cashback' | 'referral_reward' | 'pos_reversal' | 'payout' | 'adjustment';

export interface CreditLine {
  id: string;
  kind: CreditLineKind | (string & {});
  /** A whole number of credits (1 credit = ₹1). Magnitude for fixed-sign kinds; signed for `adjustment`. */
  credits: number;
  at: string;
  /** A short non-sensitive reference, e.g. the order number. Never a customer name or phone. */
  ref: { kind: 'order' | 'sale'; label: string } | null;
}

export interface CreditAccount {
  /** The business's balance in credits; it can be negative (cashback is netted at payout, 01 §7). */
  balance: number;
  items: CreditLine[];
  nextCursor: string | null;
}

const biz = (bid: string) => `/v1/biz/businesses/${encodeURIComponent(bid)}`;

/** `GET {B}/credit-account` (manager and above): the balance and a cursor page of lines, newest first. */
export function getCreditAccount(bid: string, cursor?: string): Promise<CreditAccount> {
  const q = new URLSearchParams({ limit: '30' });
  if (cursor) q.set('cursor', cursor);
  return request<CreditAccount>(`${biz(bid)}/credit-account?${q.toString()}`, { method: 'GET' });
}

/**
 * `GET {S}/qr-standee.pdf` (manager and above): the printable A5 standee as a Blob. Fetched with the
 * session's bearer token by `request`, so no token ever sits in a URL.
 */
export function getStandeePdf(bid: string, sid: string): Promise<Blob> {
  return request<Blob>(`${biz(bid)}/stores/${encodeURIComponent(sid)}/qr-standee.pdf`, {
    method: 'GET',
    headers: { Accept: 'application/pdf, application/problem+json' },
  });
}
