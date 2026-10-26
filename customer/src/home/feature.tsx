// Home's photo shop cards (StoreCard layout="feature", D-249): open or closed on the photo, the cashback
// over it, a rating badge, meta with icons and a footer that says what the shop gives back.
import type { DiscoverStoreCard } from '@todayz/api-client/gen/model/discoverStoreCard';
import { formatDistance } from '@todayz/i18n';
import type { StoreCardProps } from '@todayz/ui';
import { BadgePercent, Bike, MapPin, ShoppingBag, Store, Tag } from 'lucide-react';
import { useIntl } from 'react-intl';
import { Icon } from '../shell/Icon';
import { asTint, imageUrl, initials, storeHref } from './present';
import { RouterLink } from './RouterLink';

export function useFeatureCardProps(): (s: DiscoverStoreCard) => StoreCardProps {
  const intl = useIntl();
  const pct = (n: number) => String(n);
  return (s) => {
    const meta = [
      <>
        <Icon icon={MapPin} />
        {formatDistance(s.distanceM)}
      </>,
    ];
    if (s.locality)
      meta.push(
        <>
          <Icon icon={Store} />
          {s.locality}
        </>,
      );
    if (s.delivery)
      meta.push(
        <>
          <Icon icon={Bike} />
          {intl.formatMessage({ id: 'store.delivery' })}
        </>,
      );
    else if (s.pickup)
      meta.push(
        <>
          <Icon icon={ShoppingBag} />
          {intl.formatMessage({ id: 'store.pickup' })}
        </>,
      );
    return {
      layout: 'feature',
      name: s.name,
      initials: initials(s.name),
      tint: asTint(s.categoryTint),
      imageUrl: imageUrl(s.photoKey),
      meta,
      tags: s.bookable ? [] : [{ kind: 'plain', label: intl.formatMessage({ id: 'store.inStoreOnly' }) }],
      photoTag: s.open
        ? { kind: 'open', label: intl.formatMessage({ id: 'store.open' }) }
        : { kind: 'plain', label: intl.formatMessage({ id: 'store.closed' }) },
      highlight:
        s.cashbackPct > 0
          ? {
              title: intl.formatMessage({ id: 'home.card.cashback' }, { pct: pct(s.cashbackPct) }),
              sub: intl.formatMessage({ id: 'home.card.asCredits' }),
            }
          : undefined,
      rating:
        s.rating !== null
          ? {
              value: s.rating.toFixed(1),
              label: intl.formatMessage({ id: 'home.card.rating' }, { rating: s.rating.toFixed(1), count: s.ratingCount }),
            }
          : undefined,
      footer:
        s.cashbackPct > 0
          ? { kind: 'offer', icon: BadgePercent, label: intl.formatMessage({ id: 'home.card.footer.cashback' }, { pct: pct(s.cashbackPct) }) }
          : { kind: 'plain', icon: Tag, label: intl.formatMessage({ id: 'home.card.footer.none' }) },
      href: storeHref(s.storeSlug),
      LinkComponent: RouterLink,
    };
  };
}
