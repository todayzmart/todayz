import { API_URL } from '../app/env';

/** `GET /v1/public/ref/{shortCode}?k=s`: the store standee's QR link (referrals `link`, D-26). */
export function storeLink(shortCode: string): string {
  return `${API_URL}/v1/public/ref/${encodeURIComponent(shortCode)}?k=s`;
}
