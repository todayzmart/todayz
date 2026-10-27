// How discover data looks: shared by Home, Search, the shop page and the product page.
import type { DiscoverStoreCard } from '@todayz/api-client/gen/model/discoverStoreCard';
import { formatDistance } from '@todayz/i18n';
import type { StoreCardProps, Tint } from '@todayz/ui';
import { useIntl } from 'react-intl';
import { IMAGE_BASE_URL } from '../app/env';
import { RouterLink } from './RouterLink';

export type RadiusM = 1000 | 2000 | 5000 | 10000 | 20000;
export function radiusM(km: number): RadiusM {
  return (km * 1000) as RadiusM;
}

const TINTS: readonly string[] = ['green', 'amber', 'rose', 'sky', 'lilac', 'peach'];
export function asTint(t: string | null | undefined): Tint | undefined {
  return t && TINTS.includes(t) ? (t as Tint) : undefined;
}

/** Two letters for the avatar. */
export function initials(name: string): string {
  // Words that start with a letter, so "100% Pure coconut oil" reads PC, not 1P.
  const all = name.trim().split(/\s+/).filter(Boolean);
  const lettered = all.filter((w) => /^\p{L}/u.test(w));
  const words = lettered.length > 0 ? lettered : all;
  const first = words[0] ?? '';
  const second = words[1] ?? '';
  const out = words.length > 1 ? `${[...first][0] ?? ''}${[...second][0] ?? ''}` : [...first].slice(0, 2).join('');
  return out.toUpperCase();
}

/** The image URL for an API `imageKey`, or undefined when the item has none (the tile keeps its initials). */
export function imageUrl(key: string | null | undefined): string | undefined {
  if (!key || IMAGE_BASE_URL === '') return undefined;
  return `${IMAGE_BASE_URL}/${key.split('/').map(encodeURIComponent).join('/')}`;
}

/** Opens the system maps for a point (directions); no coordinates of the customer are sent. */
export function directionsUrl(lat: number, lng: number): string {
  return `https://www.google.com/maps/dir/?api=1&destination=${lat},${lng}`;
}

export function storeHref(slug: string, search?: string): string {
  return `/s/${encodeURIComponent(slug)}${search ? `?${search}` : ''}`;
}

/** The StoreCard props for one shop of a list (C-03, C-04, C-06). */
export function useStoreCardProps(): (s: DiscoverStoreCard) => StoreCardProps {
  const intl = useIntl();
  return (s) => {
    const meta: string[] = [formatDistance(s.distanceM)];
    if (s.locality) meta.push(s.locality);
    if (s.rating !== null) meta.push(intl.formatMessage({ id: 'store.rating' }, { rating: s.rating.toFixed(1), count: String(s.ratingCount) }));
    const tags: NonNullable<StoreCardProps['tags']>[number][] = [
      s.open
        ? { kind: 'open', label: intl.formatMessage({ id: 'store.open' }) }
        : { kind: 'plain', label: intl.formatMessage({ id: 'store.closed' }) },
    ];
    if (s.cashbackPct > 0) tags.push({ kind: 'offer', label: intl.formatMessage({ id: 'store.cashback' }, { pct: String(s.cashbackPct) }) });
    if (s.delivery) tags.push({ kind: 'plain', label: intl.formatMessage({ id: 'store.delivery' }) });
    if (!s.bookable) tags.push({ kind: 'plain', label: intl.formatMessage({ id: 'store.inStoreOnly' }) });
    return {
      name: s.name,
      initials: initials(s.name),
      tint: asTint(s.categoryTint),
      imageUrl: imageUrl(s.photoKey),
      meta,
      tags,
      href: storeHref(s.storeSlug),
      LinkComponent: RouterLink,
    };
  };
}
