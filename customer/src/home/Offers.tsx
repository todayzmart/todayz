// C-03 "Offers near you" (D-249): a swipe row of photo banners. How TodayZ works first, then the
// nearby shops' banners and best offers (one per shop, from /v1/discover/offers), then credits and
// baskets. Copy carries no configurable number (G7); offer titles come from the shops.
import { listNearbyOffers } from '@todayz/api-client/gen/offers/offers';
import type { NearbyOffer } from '@todayz/api-client/gen/model/nearbyOffer';
import { formatDistance } from '@todayz/i18n';
import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import type { Coords } from './coords';
import { PHOTOS, tintPhoto } from './photos';
import { asTint, imageUrl, radiusM, storeHref } from './present';
import { RouterLink } from './RouterLink';
import styles from './home.module.css';

const MAX_SHOP_CARDS = 6;
const TONES = ['plum', 'blue', 'rose'] as const;

interface Card {
  key: string;
  tone: 'brand' | (typeof TONES)[number];
  eyebrow: string;
  title: string;
  sub?: string | undefined;
  cta: string;
  href: string;
  photo: string;
}

/** The banner's own image, else its template's product image, else a photo for the shop's category. */
function offerPhoto(o: NearbyOffer): string {
  const template = o.banner?.template;
  const product = template && typeof template['productImageKey'] === 'string' ? template['productImageKey'] : null;
  return imageUrl(o.banner?.imageKey) ?? imageUrl(product) ?? tintPhoto(asTint(o.tint));
}

export function Offers({ coords, km }: { coords: Coords; km: number }) {
  const intl = useIntl();
  const [active, setActive] = useState(0);
  const offers = useQuery({
    queryKey: ['discover', 'offers', coords.lat, coords.lng, km],
    queryFn: () => listNearbyOffers({ lat: coords.lat, lng: coords.lng, radius: radiusM(km) }),
  });

  const seen = new Set<string>();
  const shopCards: Card[] = [];
  for (const o of offers.data?.items ?? []) {
    const title = o.banner?.headline ?? o.offer?.title;
    if (!title || seen.has(o.storeId) || shopCards.length >= MAX_SHOP_CARDS) continue;
    seen.add(o.storeId);
    shopCards.push({
      key: o.storeId,
      tone: TONES[shopCards.length % TONES.length] ?? 'plum',
      eyebrow: o.storeName,
      title,
      sub: [formatDistance(o.distanceM), o.locality].filter(Boolean).join(' · '),
      cta: intl.formatMessage({ id: 'home.offers.viewShop' }),
      href: storeHref(o.storeSlug),
      photo: offerPhoto(o),
    });
  }

  const cards: Card[] = [
    {
      key: 'how',
      tone: 'brand',
      eyebrow: intl.formatMessage({ id: 'home.offers.how.eyebrow' }),
      title: intl.formatMessage({ id: 'home.offers.how.title' }),
      sub: intl.formatMessage({ id: 'home.offers.how.sub' }),
      cta: intl.formatMessage({ id: 'home.offers.how.cta' }),
      href: '/search',
      photo: PHOTOS.shop,
    },
    ...shopCards,
    {
      key: 'credits',
      tone: 'blue',
      eyebrow: intl.formatMessage({ id: 'home.offers.credits.eyebrow' }),
      title: intl.formatMessage({ id: 'home.offers.credits.title' }),
      sub: intl.formatMessage({ id: 'home.offers.credits.sub' }),
      cta: intl.formatMessage({ id: 'home.offers.credits.cta' }),
      href: '/wallet',
      photo: PHOTOS.credits,
    },
    {
      key: 'baskets',
      tone: 'plum',
      eyebrow: intl.formatMessage({ id: 'home.offers.baskets.eyebrow' }),
      title: intl.formatMessage({ id: 'basket.entry' }),
      sub: intl.formatMessage({ id: 'basket.entry.body' }),
      cta: intl.formatMessage({ id: 'home.offers.baskets.cta' }),
      href: '/baskets',
      photo: PHOTOS.basket,
    },
  ];

  return (
    <section className={styles.offers} aria-labelledby="home-offers">
      <h2 id="home-offers" className="tz-ui-sr-only">
        {intl.formatMessage({ id: 'home.offers.title' })}
      </h2>
      <ul
        className={styles.banners}
        onScroll={(e) => {
          const row = e.currentTarget;
          const first = row.firstElementChild as HTMLElement | null;
          const step = first ? first.offsetWidth + parseFloat(getComputedStyle(row).columnGap || '0') : row.clientWidth;
          setActive(Math.min(cards.length - 1, Math.round(row.scrollLeft / Math.max(1, step))));
        }}
      >
        {cards.map((c) => (
          <li key={c.key} className={styles.bannerItem}>
            <RouterLink href={c.href} className={styles.banner} data-tone={c.tone}>
              <span className={styles.bannerText}>
                <small className={styles.bannerEyebrow}>{c.eyebrow}</small>
                <b className={styles.bannerTitle}>{c.title}</b>
                {c.sub && <span className={styles.bannerSub}>{c.sub}</span>}
                <span className={styles.bannerCta}>{c.cta}</span>
              </span>
              <img className={styles.bannerImg} src={c.photo} alt="" loading="lazy" decoding="async" />
            </RouterLink>
          </li>
        ))}
      </ul>
      <div className={styles.dots} aria-hidden="true">
        {cards.map((c, i) => (
          <span key={c.key} data-active={i === active ? '' : undefined} />
        ))}
      </div>
    </section>
  );
}
