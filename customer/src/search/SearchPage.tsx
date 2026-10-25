// C-05 Search: suggestions, results grouped by shop with proof (summary line, freshness, one badge),
// filters, refine chips (empty until the API fills them), recent searches kept on this device.
import { listCategories } from '@todayz/api-client/gen/catalogue/catalogue';
import { searchDiscover, searchSuggest } from '@todayz/api-client/gen/discover/discover';
import type { DiscoverSearchStore } from '@todayz/api-client/gen/model/discoverSearchStore';
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import {
  Banner,
  Button,
  Chip,
  ChipGroup,
  EmptyState,
  RadiusChips,
  SearchField,
  Segmented,
  Skeleton,
  StoreCard,
  type StoreCardProps,
} from '@todayz/ui';
import { MapPin, SearchX, X } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { useCoords, type Coords } from '../home/coords';
import { useFmt, usePackLine } from '../home/format';
import { radiusM, useStoreCardProps } from '../home/present';
import { RADIUS_OPTIONS_KM, setRadiusKm, useRadiusKm } from '../home/radius';
import { CartBarLink } from '../store/CartBarLink';
import { ItemCard } from '../store/ItemCard';
import { seedItems } from '../store/itemCache';
import { useCartActions } from '../store/useCartActions';
import { addRecent, clearRecent, getRecent } from './recent';
import type { AiSearchChip } from '@todayz/api-client/gen/model/aiSearchChip';
import type { AssistPick } from '../search-assist/SearchAssist';
import { useVoiceOutcome } from '../voice-search/session';
import { AskNearbySlot, SearchAssistSlot, VoiceSearchSlot } from './slots';
import styles from './search.module.css';

export type SortKey = 'relevance' | 'nearest' | 'cheapest';

export interface SearchState {
  q?: string | undefined;
  sort?: SortKey | undefined;
  open?: true | undefined;
  pickup?: true | undefined;
  delivery?: true | undefined;
  offers?: true | undefined;
  category?: string | undefined;
  /** false: the customer removed a refine chip, so the words are not parsed into filters again. */
  parse?: false | undefined;
  /** A top price a search assist chip set (AI-C2). */
  maxPricePaise?: number | undefined;
  /** A brand slug a search assist chip set (AI-C2). */
  brand?: string | undefined;
  /** The query came from voice search (C-23); recorded by the search, dropped on the next change. */
  source?: 'voice' | undefined;
}

type FilterKey = 'open' | 'pickup' | 'delivery' | 'offers';
const FILTERS: readonly FilterKey[] = ['open', 'pickup', 'delivery', 'offers'];

function clean(s: SearchState): SearchState {
  const out: SearchState = {};
  if (s.q) out.q = s.q;
  if (s.sort && s.sort !== 'relevance') out.sort = s.sort;
  for (const f of FILTERS) if (s[f]) out[f] = true;
  if (s.category) out.category = s.category;
  if (s.parse === false) out.parse = false;
  if (s.maxPricePaise && s.maxPricePaise > 0) out.maxPricePaise = s.maxPricePaise;
  if (s.brand) out.brand = s.brand;
  if (s.source === 'voice') out.source = 'voice';
  return out;
}

/** A search assist tap: the cleaned words, or one more filter. The words are not parsed again. */
function applyPick(state: SearchState, pick: AssistPick): SearchState {
  const next: SearchState = { ...state, parse: false, source: undefined };
  if (pick.kind === 'query') return { ...next, q: pick.query.slice(0, 120) };
  const { kind, value } = pick.chip;
  switch (kind) {
    case 'openNow':
      return { ...next, open: true };
    case 'pickup':
      return { ...next, pickup: true };
    case 'delivery':
      return { ...next, delivery: true };
    case 'offers':
      return { ...next, offers: true };
    case 'nearest':
      return { ...next, sort: 'nearest' };
    case 'maxPrice': {
      const paise = Number(value);
      return Number.isInteger(paise) && paise > 0 ? { ...next, maxPricePaise: paise } : state;
    }
    case 'brand':
      return value && BRAND_SLUG.test(value) ? { ...next, brand: value } : state;
  }
}

const BRAND_SLUG = /^[a-z0-9]+(-[a-z0-9]+)*$/;

/** True when tapping the chip would change no filter (already applied, or not usable). */
function chipChangesNothing(state: SearchState, chip: AiSearchChip): boolean {
  const next = applyPick(state, { kind: 'chip', chip });
  const keys = new Set([...Object.keys(state), ...Object.keys(next)] as (keyof SearchState)[]);
  keys.delete('parse');
  keys.delete('source');
  return [...keys].every((k) => next[k] === state[k]);
}

export function SearchPage({ state, onChange }: { state: SearchState; onChange: (next: SearchState) => void }) {
  const intl = useIntl();
  const coords = useCoords();
  const q = state.q ?? '';
  const [draft, setDraft] = useState(q);
  const [typed, setTyped] = useState(q);
  const [recent, setRecent] = useState<string[]>(getRecent);
  const [voiceNotice, setVoiceNotice] = useState<string | null>(null);
  const voice = useVoiceOutcome();

  // The URL is the source of truth (back button, shared links): follow it when it changes under us.
  const [seenQ, setSeenQ] = useState(q);
  if (seenQ !== q) {
    setSeenQ(q);
    setDraft(q);
    setTyped(q);
  }

  const commit = (text: string) => {
    const t = text.trim();
    setDraft(t);
    setTyped(t);
    if (t.length >= 2) setRecent(addRecent(t));
    setVoiceNotice(null);
    const source = t ? voice.searched(t) : undefined;
    onChange(clean({ ...state, q: t || undefined, parse: undefined, source }));
  };

  const suggest = useQuery({
    queryKey: ['discover', 'suggest', typed],
    enabled: typed.length >= 2 && typed !== q,
    staleTime: 5 * 60_000,
    queryFn: () => searchSuggest({ q: typed, limit: 6 }),
  });
  const suggesting = typed.length >= 2 && typed !== q;

  return (
    <div className={styles.page}>
      <header className={`tz-hero-wash ${styles.head}`}>
        <h1 className={styles.title}>{intl.formatMessage({ id: 'search.title' })}</h1>
        <form
          role="search"
          onSubmit={(e) => {
            e.preventDefault();
            commit(draft);
          }}
        >
          <SearchField
            label={intl.formatMessage({ id: 'search.field.label' })}
            placeholder={intl.formatMessage({ id: 'search.field.placeholder' })}
            clearLabel={intl.formatMessage({ id: 'search.field.clear' })}
            value={draft}
            onChange={setDraft}
            onSearch={setTyped}
            enterKeyHint="search"
            trailing={
              <VoiceSearchSlot
                onNotice={setVoiceNotice}
                onTranscript={(heard) => {
                  voice.heard(heard);
                  setDraft(heard.transcript);
                }}
              />
            }
          />
          {voiceNotice && (
            <p className={styles.voiceNotice} role="status">
              {voiceNotice}
            </p>
          )}
        </form>
      </header>

      <div className={styles.body}>
        {suggesting ? (
          <section aria-label={intl.formatMessage({ id: 'search.suggestions.label' })}>
            {suggest.data && suggest.data.items.length > 0 ? (
              <ul className={styles.suggestions}>
                {suggest.data.items.map((s, i) => (
                  <li key={`${s.kind}-${s.text}-${i}`}>
                    <button type="button" className={styles.suggestion} onClick={() => commit(s.text)}>
                      {s.text}
                    </button>
                  </li>
                ))}
              </ul>
            ) : (
              <Button variant="soft" block onClick={() => commit(typed)}>
                {intl.formatMessage({ id: 'search.suggestions.search' }, { query: typed })}
              </Button>
            )}
          </section>
        ) : q.length < 2 ? (
          <Idle recent={recent} onPick={commit} onClear={() => { clearRecent(); setRecent([]); }} />
        ) : coords.status === 'ready' ? (
          <Results coords={coords.coords} state={state} onChange={(s) => onChange(clean({ ...s, source: undefined }))} />
        ) : (
          <NoPlace status={coords.status} retry={coords.status === 'error' ? coords.retry : undefined} />
        )}
      </div>
      <CartBarLink />
    </div>
  );
}

function Idle({ recent, onPick, onClear }: { recent: string[]; onPick: (q: string) => void; onClear: () => void }) {
  const intl = useIntl();
  if (recent.length === 0) {
    return (
      <p className={styles.hint}>{intl.formatMessage({ id: 'search.idle.hint' })}</p>
    );
  }
  return (
    <section aria-labelledby="search-recent">
      <div className={styles.recentHead}>
        <h2 id="search-recent" className={styles.sectionTitle}>
          {intl.formatMessage({ id: 'search.recent.title' })}
        </h2>
        <Button variant="ghost" size="sm" onClick={onClear}>
          {intl.formatMessage({ id: 'search.recent.clear' })}
        </Button>
      </div>
      <div className="tz-chips">
        {recent.map((r) => (
          <Chip key={r} pressed={false} onPressedChange={() => onPick(r)}>
            {r}
          </Chip>
        ))}
      </div>
    </section>
  );
}

function NoPlace({ status, retry }: { status: 'none' | 'loading' | 'error'; retry?: (() => void) | undefined }) {
  const intl = useIntl();
  if (status === 'loading') return <Skeleton shape="block" height="96px" />;
  return (
    <EmptyState
      icon={MapPin}
      title={intl.formatMessage({ id: status === 'error' ? 'home.placeError.title' : 'home.noPlace.title' })}
      description={intl.formatMessage({ id: status === 'error' ? 'home.placeError.body' : 'home.noPlace.body' })}
      action={
        status === 'error' && retry ? (
          <Button onClick={retry}>{intl.formatMessage({ id: 'home.retry' })}</Button>
        ) : (
          <Link to="/location" className="tz-btn tz-btn-primary">
            {intl.formatMessage({ id: 'location.set' })}
          </Link>
        )
      }
    />
  );
}

function Results({ coords, state, onChange }: { coords: Coords; state: SearchState; onChange: (s: SearchState) => void }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const km = useRadiusKm();
  const { money } = useFmt();
  const q = state.q ?? '';
  const sort: SortKey = state.sort ?? 'relevance';
  const { lat, lng } = coords;

  const categories = useQuery({
    queryKey: ['catalogue', 'categories'],
    staleTime: 60 * 60_000,
    queryFn: () => listCategories(),
  });
  const search = useInfiniteQuery({
    queryKey: ['discover', 'search', q, lat, lng, km, sort, state.open, state.pickup, state.delivery, state.offers, state.category, state.parse, state.maxPricePaise, state.brand, state.source],
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
      searchDiscover({
        q,
        lat,
        lng,
        radius: radiusM(km),
        sort,
        ...(state.open ? { openNow: true } : {}),
        ...(state.pickup ? { pickup: true } : {}),
        ...(state.delivery ? { delivery: true } : {}),
        ...(state.offers ? { hasOffer: true } : {}),
        ...(state.category ? { category: state.category } : {}),
        ...(state.parse === false ? { parse: false } : {}),
        ...(state.maxPricePaise ? { maxPricePaise: state.maxPricePaise } : {}),
        ...(state.brand ? { brand: state.brand } : {}),
        ...(state.source ? { source: state.source } : {}),
        limit: 20,
        ...(pageParam ? { cursor: pageParam } : {}),
      }),
  });

  const pages = useMemo(() => search.data?.pages ?? [], [search.data]);
  const first = pages[0];
  const groups = pages.flatMap((p) => p.results.map((r) => ({ r, serverNow: p.serverNow })));
  // C-05 "none confirmed" (D-190): shops have it listed, but none has confirmed the stock lately
  // (the server nulls `confirmedAt` past `stock_stale_after_hours`).
  const noneConfirmed =
    groups.length > 0 && !groups.some(({ r }) => r.items.some((i) => i.inStock && i.confirmedAt !== null));

  useEffect(() => {
    for (const p of pages) for (const r of p.results) seedItems(qc, r.store.storeSlug, r.items, p.serverNow);
  }, [pages, qc]);

  const filterValue = FILTERS.filter((f) => state[f]);
  const filterLabels: Record<FilterKey, string> = {
    open: intl.formatMessage({ id: 'search.filter.open' }),
    pickup: intl.formatMessage({ id: 'search.filter.pickup' }),
    delivery: intl.formatMessage({ id: 'search.filter.delivery' }),
    offers: intl.formatMessage({ id: 'search.filter.offers' }),
  };
  const cats = categories.data?.items ?? [];

  return (
    <>
      <div className={styles.filters}>
        <RadiusChips
          label={intl.formatMessage({ id: 'home.radius.label' })}
          options={RADIUS_OPTIONS_KM.map((k) => ({ km: k, label: intl.formatMessage({ id: 'home.radius.km' }, { km: String(k) }) }))}
          value={km}
          onChange={setRadiusKm}
        />
        <ChipGroup
          multiple
          label={intl.formatMessage({ id: 'search.filter.label' })}
          options={FILTERS.map((f) => ({ value: f, label: filterLabels[f] }))}
          value={filterValue}
          onChange={(next) => {
            const s: SearchState = { ...state };
            for (const f of FILTERS) s[f] = next.includes(f) ? true : undefined;
            onChange(s);
          }}
        />
        {cats.length > 0 && (
          <ChipGroup
            label={intl.formatMessage({ id: 'search.category.label' })}
            options={[{ value: '', label: intl.formatMessage({ id: 'search.category.all' }) }, ...cats.map((c) => ({ value: c.slug, label: c.name }))]}
            value={state.category ?? ''}
            onChange={(v) => onChange({ ...state, category: v || undefined })}
          />
        )}
        <Segmented
          label={intl.formatMessage({ id: 'search.sort.label' })}
          options={[
            { value: 'relevance', label: intl.formatMessage({ id: 'search.sort.relevance' }) },
            { value: 'nearest', label: intl.formatMessage({ id: 'search.sort.nearest' }) },
            { value: 'cheapest', label: intl.formatMessage({ id: 'search.sort.cheapest' }) },
          ]}
          value={sort}
          onChange={(v) => onChange({ ...state, sort: v })}
        />
      </div>

      {(state.maxPricePaise || state.brand) && (
        <div className="tz-chips" role="group" aria-label={intl.formatMessage({ id: 'search.assist.applied' })}>
          {state.maxPricePaise ? (
            <Chip pressed icon={X} onPressedChange={() => onChange({ ...state, maxPricePaise: undefined })}>
              {intl.formatMessage({ id: 'search.assist.maxPrice' }, { price: money(state.maxPricePaise) })}
            </Chip>
          ) : null}
          {state.brand ? (
            <Chip pressed icon={X} onPressedChange={() => onChange({ ...state, brand: undefined })}>
              {intl.formatMessage({ id: 'search.assist.brand' }, { brand: state.brand })}
            </Chip>
          ) : null}
        </div>
      )}

      {first && first.chips.length > 0 && (
        <div className="tz-chips" role="group" aria-label={intl.formatMessage({ id: 'search.refine.label' })}>
          {first.chips.map((c, i) => (
            <Chip
              key={`${c.kind}-${i}`}
              pressed
              icon={X}
              aria-label={intl.formatMessage({ id: 'search.refine.remove' }, { chip: c.text })}
              // TODO(lead): the API does not say which words made which chip, so removing any chip re-runs
              // the search with parse=false (no chips) instead of dropping just that one.
              onPressedChange={() => onChange({ ...state, parse: false })}
            >
              {c.text}
            </Chip>
          ))}
        </div>
      )}

      {first?.summary && first.summary.storesWithIt > 0 && (
        <p className={styles.summary} role="status">
          {intl.formatMessage(
            { id: 'search.summary' },
            {
              checked: String(first.summary.storesChecked),
              have: String(first.summary.storesWithIt),
              min: money(first.summary.minPricePaise),
            },
          )}
        </p>
      )}
      {first && (
        <SearchAssistSlot
          query={q}
          refineHint={first.refineHint}
          hide={(c) => chipChangesNothing(state, c)}
          onPick={(p) => onChange(applyPick(state, p))}
        />
      )}

      {search.isPending && (
        <div aria-busy="true" className={styles.groups}>
          <Skeleton shape="block" height="160px" />
          <Skeleton shape="block" height="160px" />
        </div>
      )}
      {search.isError && (
        <EmptyState
          title={intl.formatMessage({ id: 'search.error' })}
          action={<Button onClick={() => void search.refetch()}>{intl.formatMessage({ id: 'home.retry' })}</Button>}
        />
      )}
      {search.isSuccess && groups.length === 0 && (
        <>
          <EmptyState
            icon={SearchX}
            title={intl.formatMessage({ id: 'search.zero.title' }, { query: q })}
            description={intl.formatMessage({ id: 'search.zero.body' })}
          />
          <AskNearbySlot query={q} />
        </>
      )}
      {search.isSuccess && !search.hasNextPage && noneConfirmed && (
        <NoneConfirmed km={km} query={q} />
      )}
      <ul className={styles.groups}>
        {groups.map(({ r, serverNow }) => (
          <li key={r.store.storeSlug}>
            <ResultGroup result={r} serverNow={serverNow} />
          </li>
        ))}
      </ul>
      {search.hasNextPage && (
        <Button variant="soft" block loading={search.isFetchingNextPage} onClick={() => void search.fetchNextPage()}>
          {intl.formatMessage({ id: 'search.more' })}
        </Button>
      )}
    </>
  );
}

/**
 * D-190: say plainly that no shop in range has confirmed the stock, and offer the next radius up. At 20 km
 * there is no wider option, so offer to search again later. The shops are never asked again from here:
 * "Ask shops nearby" stays the customer's own action (AskNearbySlot).
 */
function NoneConfirmed({ km, query }: { km: number; query: string }) {
  const intl = useIntl();
  const next = RADIUS_OPTIONS_KM.find((k) => k > km);
  const qc = useQueryClient();
  return (
    <>
      <Banner
        tone="warning"
        live="status"
        title={intl.formatMessage({ id: 'search.noneConfirmed.title' })}
      >
        {intl.formatMessage({ id: next !== undefined ? 'search.noneConfirmed.body' : 'search.noneConfirmed.bodyWidest' })}
      </Banner>
      {next !== undefined ? (
        <Button variant="soft" block onClick={() => setRadiusKm(next)}>
          {intl.formatMessage({ id: 'search.noneConfirmed.wider' }, { km: String(next) })}
        </Button>
      ) : (
        <Button variant="soft" block onClick={() => void qc.invalidateQueries({ queryKey: ['discover', 'search', query] })}>
          {intl.formatMessage({ id: 'search.noneConfirmed.later' })}
        </Button>
      )}
      <AskNearbySlot query={query} />
    </>
  );
}

function ResultGroup({ result, serverNow }: { result: DiscoverSearchStore; serverNow: string }) {
  const intl = useIntl();
  const { money } = useFmt();
  const cardProps = useStoreCardProps();
  const packLine = usePackLine();
  const { qtyOf, change, sheet } = useCartActions();
  const { store } = result;

  const base = cardProps(store);
  const tags: NonNullable<StoreCardProps['tags']>[number][] = [];
  // At most one badge per card (01 §11.1); labels only, they never change the order.
  if (result.badge === 'lowest_price') tags.push({ kind: 'offer', label: intl.formatMessage({ id: 'search.badge.lowest' }) });
  else if (result.badge === 'closest') tags.push({ kind: 'offer', label: intl.formatMessage({ id: 'search.badge.closest' }) });
  if (result.deliveryFeePaise !== null && result.deliveryFeePaise > 0) {
    tags.push({ kind: 'plain', label: intl.formatMessage({ id: 'search.deliveryFee' }, { fee: money(result.deliveryFeePaise) }) });
  }

  return (
    <section className={styles.group} aria-label={store.name}>
      <StoreCard {...base} tags={[...tags, ...(base.tags ?? [])]} />
      <div className={styles.items}>
        {result.items.map((item) => (
          <ItemCard
            key={item.storeItemId}
            shopSlug={store.storeSlug}
            item={item}
            serverNow={serverNow}
            bookable={store.bookable}
            qty={qtyOf(store.storeSlug, item.storeItemId)}
            onChange={(qty) => change({ id: store.storeId, slug: store.storeSlug, name: store.name }, item, packLine(item), qty)}
          />
        ))}
      </div>
      {sheet}
    </section>
  );
}
