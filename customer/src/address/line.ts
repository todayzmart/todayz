import type { Address } from '@todayz/api-client/gen/model/address';

/** "12, 4th Cross, Near the park, 560038": what the rider reads, without the label. */
export function addressLine(a: Pick<Address, 'line1' | 'line2' | 'landmark' | 'pincode'>): string {
  return [a.line1, a.line2, a.landmark, a.pincode].filter((s): s is string => Boolean(s)).join(', ');
}
