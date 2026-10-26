// AI-C3 substitutes (W3-12, D-233): on an out-of-stock item in C-07, C-08 and C-09, "See similar items"
// opens a sheet with similar in-stock items of the same shop and the same product at shops nearby. The
// list is rule-based (no model); nothing changes in the cart until the customer opens an item and adds it.
import { listDiscoverSubstitutes } from '@todayz/api-client/gen/discover/discover';
import type { DiscoverSearchItem } from '@todayz/api-client/gen/model/discoverSearchItem';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { formatDistance } from '@todayz/i18n';
import { Button, EmptyState, Sheet, Skeleton } from '@todayz/ui';
import { Shuffle } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { useCoords } from '../home/coords';
import { useFmt, usePackLine } from '../home/format';
import { radiusM } from '../home/present';
import { useRadiusKm } from '../home/radius';
import { RouterLink } from '../home/RouterLink';
import { itemHref } from '../store/itemUi';
import { seedItems } from '../store/itemCache';
import styles from './substitutes.module.css';

export interface SubstitutesProps {
  shopSlug: string;
  /** The out-of-stock item (`storeItemId`). */
  storeItemId: string;
  name: string;
  size?: 'sm' | 'md';
}

/** The button and its sheet. Hidden until the place has a point, which the API needs. */
export function Substitutes({ shopSlug, storeItemId, name, size = 'sm' }: SubstitutesProps) {
  const intl = useIntl();
  const qc = useQueryClient();
  const { money } = useFmt();
  const packLine = usePackLine();
  const coordsState = useCoords();
  const coords = coordsState.status === 'ready' ? coordsState.coords : null;
  const km = useRadiusKm();
  const [open, setOpen] = useState(false);

  const subs = useQuery({
    queryKey: ['discover', 'substitutes', shopSlug, storeItemId, coords?.lat ?? null, coords?.lng ?? null, km],
    enabled: open && coords !== null,
    staleTime: 60_000,
    retry: false,
    queryFn: async () => {
      if (!coords) throw new Error('no_place');
      const r = await listDiscoverSubstitutes(shopSlug, { itemId: storeItemId, lat: coords.lat, lng: coords.lng, radius: radiusM(km) });
      seedItems(qc, shopSlug, r.sameShop, r.serverNow);
      for (const s of r.nearby) seedItems(qc, s.store.storeSlug, s.items, r.serverNow);
      return r;
    },
  });

  if (!coords) return null;
  const close = () => setOpen(false);

  const row = (slug: string, it: DiscoverSearchItem, title: string, meta: string) => (
    <li key={`${slug}/${it.storeItemId}`}>
      <RouterLink href={itemHref(slug, it.storeItemId)} className={styles.row} onClick={close}>
        <span className="tz-ui-minw0">
          <b>{title}</b>
          <span className="tz-meta">
            <span>{meta}</span>
          </span>
        </span>
        <span className="tz-price">{money(it.pricePaise)}</span>
      </RouterLink>
    </li>
  );

  let body;
  if (subs.isPending) {
    body = (
      <div className={styles.body} aria-busy="true">
        <Skeleton shape="block" height="56px" />
        <Skeleton shape="block" height="56px" />
      </div>
    );
  } else if (subs.isError) {
    body = (
      <div className={styles.body}>
        <p className="tz-meta">{intl.formatMessage({ id: 'substitutes.error' })}</p>
        <Button variant="soft" size="sm" onClick={() => void subs.refetch()}>
          {intl.formatMessage({ id: 'substitutes.retry' })}
        </Button>
      </div>
    );
  } else if (subs.data.sameShop.length === 0 && subs.data.nearby.length === 0) {
    body = <EmptyState icon={Shuffle} headingLevel={3} title={intl.formatMessage({ id: 'substitutes.none' })} />;
  } else {
    const { sameShop, nearby } = subs.data;
    body = (
      <div className={styles.body}>
        {sameShop.length > 0 && (
          <section aria-labelledby="subs-same">
            <h3 id="subs-same" className={styles.heading}>
              {intl.formatMessage({ id: 'substitutes.sameShop' })}
            </h3>
            <ul className={styles.list}>{sameShop.map((it) => row(shopSlug, it, it.name, packLine(it)))}</ul>
          </section>
        )}
        {nearby.length > 0 && (
          <section aria-labelledby="subs-nearby">
            <h3 id="subs-nearby" className={styles.heading}>
              {intl.formatMessage({ id: 'substitutes.nearby' })}
            </h3>
            <ul className={styles.list}>
              {nearby.flatMap((s) => s.items.slice(0, 1).map((it) => row(s.store.storeSlug, it, s.store.name, formatDistance(s.store.distanceM))))}
            </ul>
          </section>
        )}
      </div>
    );
  }

  return (
    <>
      <Button variant="soft" size={size} icon={Shuffle} onClick={() => setOpen(true)}>
        {intl.formatMessage({ id: 'substitutes.open' })}
      </Button>
      <Sheet
        open={open}
        onClose={close}
        title={intl.formatMessage({ id: 'substitutes.title' }, { name })}
        description={intl.formatMessage({ id: 'substitutes.description' })}
        closeLabel={intl.formatMessage({ id: 'substitutes.close' })}
      >
        {body}
      </Sheet>
    </>
  );
}
