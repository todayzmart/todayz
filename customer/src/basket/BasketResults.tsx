// C-22 results: "Search" asks POST /v1/discover/basket-search for the basket at the customer's place and radius,
// then shows the ranked shops ("{n} of {m} · {distance} · ₹{total}", what is missing, freshness per line, at
// most one badge) and, when no shop has everything, the two-shop split. The total is a preview: checkout
// reprices. Search never changes stock, money or orders; it only reads.
import { searchBasket } from '@todayz/api-client/gen/discover/discover';
import type { BasketCoveredLine } from '@todayz/api-client/gen/model/basketCoveredLine';
import type { BasketSearchResult } from '@todayz/api-client/gen/model/basketSearchResult';
import type { BasketSearchStore } from '@todayz/api-client/gen/model/basketSearchStore';
import type { BasketSplitSuggestion } from '@todayz/api-client/gen/model/basketSplitSuggestion';
import { formatDistance } from '@todayz/i18n';
import { useMutation } from '@tanstack/react-query';
import { useRouter } from '@tanstack/react-router';
import { Button, EmptyState, RadiusChips, Sheet, Skeleton } from '@todayz/ui';
import { MapPin, Search, ShoppingCart, Store as StoreIcon } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import type { CartStore } from '../cart/cart';
import { useCoords } from '../home/coords';
import { useFmt, useUnitLabel } from '../home/format';
import { radiusM } from '../home/present';
import { RouterLink } from '../home/RouterLink';
import { RADIUS_OPTIONS_KM, setRadiusKm, useRadiusKm } from '../home/radius';
import { AskNearbySlot } from '../search/slots';
import { Icon } from '../shell/Icon';
import { fillCart, fillLines, needsReplace, setPendingSplit, type FillLine, type PendingSplit } from './fill';
import { signature, type EditLine } from './model';
import styles from './basket.module.css';

/** What the results were computed for: a change to the lines makes them stale. */
export interface BasketResultsProps {
  lines: readonly EditLine[];
  /** Called after a search, with the lines it was run for (a saved basket stores the split). */
  onResult?: ((result: BasketSearchResult, lines: readonly EditLine[]) => void) | undefined;
}

interface Ask {
  store: CartStore;
  lines: FillLine[];
  next: PendingSplit | null;
  current: CartStore;
}

export function BasketResults({ lines, onResult }: BasketResultsProps) {
  const intl = useIntl();
  const router = useRouter();
  const coords = useCoords();
  const km = useRadiusKm();
  const unitLabel = useUnitLabel();
  const [ran, setRan] = useState<{ sig: string; lines: readonly EditLine[]; result: BasketSearchResult } | null>(null);
  const [ask, setAsk] = useState<Ask | null>(null);

  const pack = (c: BasketCoveredLine) => {
    const p = unitLabel(c.packSize, c.unit);
    return c.variant ? `${c.variant} · ${p}` : p;
  };

  const search = useMutation({
    mutationFn: (v: { lat: number; lng: number; lines: readonly EditLine[] }) =>
      searchBasket({
        lat: v.lat,
        lng: v.lng,
        radius: radiusM(km),
        lines: v.lines.map((l) => (l.productId ? { productId: l.productId, quantity: l.quantity } : { text: l.text ?? l.label, quantity: l.quantity })),
      }),
    onSuccess: (result, v) => {
      setRan({ sig: signature(v.lines), lines: v.lines, result });
      onResult?.(result, v.lines);
    },
  });

  const go = (store: CartStore, fill: FillLine[], next: PendingSplit | null) => {
    fillCart(store, fill);
    setPendingSplit(next);
    router.history.push('/cart');
  };
  const fill = (store: CartStore, fill: FillLine[], next: PendingSplit | null) => {
    const current = needsReplace(store);
    if (current) setAsk({ store, lines: fill, next, current });
    else go(store, fill, next);
  };

  if (lines.length === 0) return null;
  if (coords.status !== 'ready') {
    if (coords.status === 'loading') return <Skeleton shape="block" height="96px" />;
    return (
      <EmptyState
        icon={MapPin}
        title={intl.formatMessage({ id: 'basket.noPlace.title' })}
        description={intl.formatMessage({ id: 'basket.noPlace.body' })}
        action={
          <RouterLink href="/location" className="tz-btn tz-btn-primary">
            {intl.formatMessage({ id: 'location.set' })}
          </RouterLink>
        }
      />
    );
  }

  const sig = signature(lines);
  const result = ran && ran.sig === sig ? ran.result : null;
  const shown = ran?.lines ?? lines;
  const labelOf = (i: number) => shown[i]?.label ?? '';

  return (
    <section className={styles.results} aria-labelledby="basket-results">
      <h2 id="basket-results" className={styles.sectionTitle}>
        {intl.formatMessage({ id: 'basket.results.title' })}
      </h2>
      <RadiusChips
        label={intl.formatMessage({ id: 'home.radius.label' })}
        options={RADIUS_OPTIONS_KM.map((k) => ({ km: k, label: intl.formatMessage({ id: 'home.radius.km' }, { km: String(k) }) }))}
        value={km}
        onChange={setRadiusKm}
      />
      <Button
        variant="primary"
        block
        icon={Search}
        loading={search.isPending}
        loadingLabel={intl.formatMessage({ id: 'basket.search.running' })}
        onClick={() => search.mutate({ lat: coords.coords.lat, lng: coords.coords.lng, lines })}
      >
        {intl.formatMessage({ id: ran ? 'basket.search.again' : 'basket.search' })}
      </Button>

      {search.isPending && (
        <p className={styles.summary} role="status">
          {ran
            ? intl.formatMessage({ id: 'basket.search.checking' }, { n: String(ran.result.summary.storesChecked) })
            : intl.formatMessage({ id: 'basket.search.checkingNearby' })}
        </p>
      )}
      {search.isError && !search.isPending && (
        <p className={styles.error} role="alert">
          {intl.formatMessage({ id: 'basket.search.error' })}
        </p>
      )}
      {ran && !result && !search.isPending && <p className={styles.hint}>{intl.formatMessage({ id: 'basket.search.stale' })}</p>}

      {result && !search.isPending && (
        <>
          <p className={styles.summary} role="status">
            {intl.formatMessage(
              { id: 'basket.summary' },
              { checked: String(result.summary.storesChecked), all: result.summary.storesWithAll },
            )}
          </p>
          {result.results.length === 0 ? (
            <>
              <EmptyState
                icon={StoreIcon}
                title={intl.formatMessage({ id: 'basket.zero.title' })}
                description={intl.formatMessage({ id: 'basket.zero.body' })}
              />
              {shown.map((l) => (
                <AskNearbySlot key={l.key} query={l.label} />
              ))}
            </>
          ) : (
            <>
              {result.split && (
                <SplitCard
                  split={result.split}
                  labelOf={labelOf}
                  pack={pack}
                  onFill={(first, second) =>
                    fill(first.store, first.lines, { afterStoreId: first.store.id, store: second.store, lines: second.lines })
                  }
                />
              )}
              <ul className={styles.cards}>
                {result.results.map((s) => (
                  <li key={s.storeId}>
                    <StoreResult
                      store={s}
                      serverNow={result.serverNow}
                      labelOf={labelOf}
                      pack={pack}
                      onFill={() => fill({ id: s.storeId, slug: s.storeSlug, name: s.storeName }, fillLines(s.covered, pack), null)}
                    />
                  </li>
                ))}
              </ul>
            </>
          )}
          <p className="tz-meta">{intl.formatMessage({ id: 'basket.preview' })}</p>
        </>
      )}

      <Sheet
        open={ask !== null}
        onClose={() => setAsk(null)}
        title={intl.formatMessage({ id: 'store.replace.title' })}
        description={ask ? intl.formatMessage({ id: 'store.replace.body' }, { current: ask.current.name, next: ask.store.name }) : undefined}
        closeLabel={intl.formatMessage({ id: 'store.replace.close' })}
        footer={
          <>
            <Button variant="ghost" onClick={() => setAsk(null)}>
              {intl.formatMessage({ id: 'store.replace.keep' })}
            </Button>
            <Button
              variant="primary"
              onClick={() => {
                const a = ask;
                setAsk(null);
                if (a) go(a.store, a.lines, a.next);
              }}
            >
              {intl.formatMessage({ id: 'store.replace.confirm' })}
            </Button>
          </>
        }
      />
    </section>
  );
}

function BadgeTag({ badge }: { badge: BasketSearchStore['badge'] }) {
  const intl = useIntl();
  if (!badge) return null;
  const id = badge === 'has_everything' ? 'basket.badge.everything' : badge === 'closest' ? 'basket.badge.closest' : 'basket.badge.lowest';
  return <span className="tz-tag tz-tag-offer">{intl.formatMessage({ id })}</span>;
}

function StoreResult({
  store,
  serverNow,
  labelOf,
  pack,
  onFill,
}: {
  store: BasketSearchStore;
  serverNow: string;
  labelOf: (i: number) => string;
  pack: (c: BasketCoveredLine) => string;
  onFill: () => void;
}) {
  const intl = useIntl();
  const { money, ago } = useFmt();
  const missing = store.missing.map((m) => labelOf(m.lineIndex)).filter(Boolean);
  return (
    <article className={`tz-card ${styles.card}`} aria-label={store.storeName}>
      <div className={styles.cardHead}>
        <RouterLink href={`/s/${encodeURIComponent(store.storeSlug)}`} className={`tz-card-title ${styles.storeName}`}>
          {store.storeName}
        </RouterLink>
        <BadgeTag badge={store.badge} />
      </div>
      <p className={styles.cardMeta}>
        {intl.formatMessage(
          { id: 'basket.card.meta' },
          { n: String(store.coveredCount), m: String(store.lineCount), distance: formatDistance(store.distanceM), total: money(store.totalPaise) },
        )}
      </p>
      <div className="tz-chips">
        <span className={store.open ? 'tz-tag tz-tag-open' : 'tz-tag'}>{intl.formatMessage({ id: store.open ? 'store.open' : 'store.closed' })}</span>
        {store.locality ? <span className="tz-tag">{store.locality}</span> : null}
        {!store.bookable ? <span className="tz-tag">{intl.formatMessage({ id: 'store.inStoreOnly' })}</span> : null}
      </div>
      <ul className={styles.covered}>
        {store.covered.map((c) => (
          <li key={c.lineIndex}>
            <span className={styles.coveredName}>
              {c.name} <span className="tz-meta">{pack(c)}</span>
            </span>
            <span className={styles.coveredQty}>
              {intl.formatMessage({ id: 'basket.card.qty' }, { qty: String(c.quantity), price: money(c.lineTotalPaise) })}
            </span>
            {c.confirmedAt ? (
              <span className="tz-fresh">
                {Date.parse(serverNow) - Date.parse(c.confirmedAt) < 60_000
                  ? intl.formatMessage({ id: 'product.confirmedNow' })
                  : intl.formatMessage({ id: 'product.confirmed' }, { time: ago(c.confirmedAt, serverNow) })}
              </span>
            ) : null}
          </li>
        ))}
      </ul>
      {missing.length > 0 && <p className={styles.missing}>{intl.formatMessage({ id: 'basket.card.missing' }, { items: missing.join(', ') })}</p>}
      {store.bookable ? (
        <Button variant="soft" icon={ShoppingCart} onClick={onFill}>
          {intl.formatMessage({ id: 'basket.fill' })}
        </Button>
      ) : (
        <RouterLink href={`/s/${encodeURIComponent(store.storeSlug)}`} className="tz-btn tz-btn-soft">
          <Icon icon={StoreIcon} />
          {intl.formatMessage({ id: 'store.viewShop' })}
        </RouterLink>
      )}
    </article>
  );
}

interface Half {
  store: CartStore;
  lines: FillLine[];
}

function SplitCard({
  split,
  labelOf,
  pack,
  onFill,
}: {
  split: BasketSplitSuggestion;
  labelOf: (i: number) => string;
  pack: (c: BasketCoveredLine) => string;
  onFill: (first: Half, second: Half) => void;
}) {
  const intl = useIntl();
  const { money } = useFmt();
  const [a, b] = split.stores;
  if (!a || !b) return null;
  const half = (p: typeof a): Half => ({ store: { id: p.storeId, slug: p.storeSlug, name: p.storeName }, lines: fillLines(p.covered, pack) });
  const uncovered = split.uncovered.map(labelOf).filter(Boolean);
  return (
    <article className={`tz-card ${styles.card} ${styles.split}`} aria-labelledby="basket-split">
      <h3 id="basket-split" className="tz-card-title">
        {intl.formatMessage({ id: 'basket.split.title' })}
      </h3>
      <p className="tz-meta">{intl.formatMessage({ id: 'basket.split.body' })}</p>
      <ol className={styles.parts}>
        {split.stores.map((p) => (
          <li key={p.storeId} className={styles.part}>
            <div className={styles.cardHead}>
              <span className={styles.storeName}>{p.storeName}</span>
              <span className={styles.partTotal}>{money(p.totalPaise)}</span>
            </div>
            <p className="tz-meta">
              {intl.formatMessage({ id: 'basket.split.meta' }, { n: String(p.lineIndexes.length), distance: formatDistance(p.distanceM) })}
            </p>
            <p className={styles.partLines}>{p.lineIndexes.map(labelOf).join(', ')}</p>
          </li>
        ))}
      </ol>
      {uncovered.length > 0 && (
        <div className={styles.uncovered}>
          <p className={styles.missing}>{intl.formatMessage({ id: 'basket.split.uncovered' }, { items: uncovered.join(', ') })}</p>
          {uncovered.map((u) => (
            <AskNearbySlot key={u} query={u} />
          ))}
        </div>
      )}
      {a.bookable && b.bookable ? (
        <Button variant="primary" icon={ShoppingCart} onClick={() => onFill(half(a), half(b))}>
          {intl.formatMessage({ id: 'basket.split.fill' }, { shop: a.storeName })}
        </Button>
      ) : (
        <p className="tz-meta">{intl.formatMessage({ id: 'basket.split.inStore' })}</p>
      )}
    </article>
  );
}
