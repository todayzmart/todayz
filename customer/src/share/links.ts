// Share helpers (C-08), apart from the component so they can be tested on their own.
import type { ShareCard } from '@todayz/api-client/gen/model/shareCard';

export function isAndroid(ua: string = typeof navigator === 'undefined' ? '' : navigator.userAgent): boolean {
  return /Android/i.test(ua);
}

export function whatsappHref(card: Pick<ShareCard, 'text' | 'url'>): string {
  return `https://wa.me/?text=${encodeURIComponent(`${card.text} ${card.url}`)}`;
}
