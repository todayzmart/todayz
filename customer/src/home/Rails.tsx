// C-03 product rails: sideways rows of item tiles built from the nearest open, bookable shops' item lists
// (the API has no "items near you" list): "From {shop}" for the nearest ones, "Deals near you" (items with
// an offer, biggest saving first) and "Under ₹99" (in stock, cheapest first). Add puts the item in the
// cart for its own shop, the same as on the shop page.
import { listDiscoverStoreItems } from '@todayz/api-client/gen/discover/discover';
import type { DiscoverSearchItem } from '@todayz/api-client/gen/model/discoverSearchItem';
import type { DiscoverStoreCard } from '@todayz/api-client/gen/model/discoverStoreCard';
import { useQueries, useQueryClient } from '@tanstack/react-query';
import { Skeleton } from '@todayz/ui';
import { ChevronRight } from 'lucide-react';
import { type ReactNode } from 'react';
import { useIntl } from 'react-intl';
import { Icon } from '../shell/Icon';
import { ItemCard } from '../store/ItemCard';
import { seedItems } from '../store/itemCache';
import { useCartActions, type ShopRef } from '../store/useCartActions';
import { offPercent, useFmt, usePackLine } from './format';
import { storeHref } from './present';
import { RouterLink } from './RouterLink';
import styles from './home.module.css';

/** How many tiles a rail shows. */
const PER_RAIL = 12;
/** "Under ₹99": a shelf label for browsing, not a product rule. */
const UNDER_PAISE = 9900;

interface Entry {
  shop: ShopRef;
  item: DiscoverSearchItem;
  serverNow: string;
}

/** The rails above the category grids ("From" the nearest shop, deals) or below them (under ₹99, the next shop). */
export function Rails({ shops, part }: { shops: readonly DiscoverStoreCard[]; part: 'top' | 'bottom' }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const { money } = useFmt();
  const lists = useQueries({
    queries: shops.map((s) => ({
      queryKey: ['discover', 'rail-items', s.storeSlug],
      staleTime: 60_000,
      queryFn: async () => {
        const r = await listDiscoverStoreItems(s.storeSlug, { limit: 24 });
        // The product page is built from what a list already returned (store/itemCache).
        seedItems(qc, s.storeSlug, r.items, r.serverNow);
        return r;
      },
    })),
  });
  const data = lists.map((l) => l.data);

  const perShop: Entry[][] = shops.map((s, i) => {
    const d = data[i];
    const shop = { id: s.storeId, slug: s.storeSlug, name: s.name };
    return d ? d.items.map((item) => ({ shop, item, serverNow: d.serverNow })) : [];
  });
  const all = perShop.flat().filter((e) => e.item.inStock);
  const pending = lists.some((l) => l.isPending);
  const saving = (e: Entry) => offPercent(e.item.pricePaise, e.item.mrpPaise) ?? 0;

  if (shops.length === 0) return null;

  const fromRail = (i: number) => {
    const s = shops[i];
    const entries = perShop[i] ?? [];
    if (!s || (entries.length === 0 && !lists[i]?.isPending)) return null;
    return (
      <Rail
        key={s.storeSlug}
        id={`rail-from-${i}`}
        title={intl.formatMessage({ id: 'home.rail.from' }, { shop: s.name })}
        more={{ href: storeHref(s.storeSlug), label: intl.formatMessage({ id: 'home.rail.seeAllAt' }, { shop: s.name }) }}
        loading={lists[i]?.isPending ?? false}
        entries={entries.slice(0, PER_RAIL)}
      />
    );
  };

  if (part === 'top') {
    const deals = all.filter((e) => e.item.hasOffer || saving(e) > 0).sort((a, b) => saving(b) - saving(a));
    return (
      <>
        {fromRail(0)}
        {(pending || deals.length > 0) && (
          <Rail id="rail-deals" title={intl.formatMessage({ id: 'home.rail.deals' })} loading={pending} entries={deals.slice(0, PER_RAIL)} mixed />
        )}
      </>
    );
  }
  const under = all.filter((e) => e.item.pricePaise <= UNDER_PAISE).sort((a, b) => a.item.pricePaise - b.item.pricePaise);
  return (
    <>
      {under.length > 0 && (
        <Rail
          id="rail-under"
          title={intl.formatMessage({ id: 'home.rail.under' }, { price: money(UNDER_PAISE) })}
          loading={false}
          entries={under.slice(0, PER_RAIL)}
          mixed
        />
      )}
      {fromRail(1)}
    </>
  );
}

function Rail({
  id,
  title,
  more,
  loading,
  entries,
  mixed = false,
}: {
  id: string;
  title: ReactNode;
  more?: { href: string; label: string } | undefined;
  loading: boolean;
  entries: readonly Entry[];
  /** Items from several shops: each tile names its shop. */
  mixed?: boolean;
}) {
  const intl = useIntl();
  const packLine = usePackLine();
  const { qtyOf, change, sheet } = useCartActions();
  return (
    <section className={styles.rail} aria-labelledby={id}>
      <div className={styles.railHead}>
        <h2 id={id} className={styles.railTitle}>
          {title}
        </h2>
        {more && (
          <RouterLink href={more.href} className={styles.seeAll} aria-label={more.label}>
            {intl.formatMessage({ id: 'home.rail.seeAll' })}
            <Icon icon={ChevronRight} />
          </RouterLink>
        )}
      </div>
      <ul className={styles.railList} aria-busy={loading}>
        {loading &&
          entries.length === 0 &&
          [0, 1, 2, 3].map((k) => (
            <li key={k} className={styles.railItem}>
              <Skeleton shape="block" height="150px" />
              <Skeleton width="50%" />
              <Skeleton width="80%" />
            </li>
          ))}
        {entries.map((e) => (
          <li key={`${e.shop.slug}/${e.item.storeItemId}`} className={styles.railItem}>
            <ItemCard
              shopSlug={e.shop.slug}
              item={e.item}
              serverNow={e.serverNow}
              bookable
              qty={qtyOf(e.shop.slug, e.item.storeItemId)}
              onChange={(qty) => change(e.shop, e.item, packLine(e.item), qty)}
              shopName={mixed ? e.shop.name : undefined}
            />
          </li>
        ))}
      </ul>
      {sheet}
    </section>
  );
}
