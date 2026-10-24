// C-22 /baskets/:basketId (a saved basket) and /baskets/device (the signed-out customer's basket on this
// device): the editor above the results. A saved basket saves itself a moment after each change, with the
// version it read (If-Match); a 412 reloads it.
import type { Basket } from '@todayz/api-client/gen/model/basket';
import type { BasketPatch } from '@todayz/api-client/gen/model/basketPatch';
import type { BasketSearchResult } from '@todayz/api-client/gen/model/basketSearchResult';
import { useSessionState } from '@todayz/auth';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Banner, Button, EmptyState, Skeleton } from '@todayz/ui';
import { ChevronLeft, ShoppingBasket } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useIntl } from 'react-intl';
import { ApiError } from '@todayz/api-client';
import { useUnitLabel } from '../home/format';
import { RouterLink } from '../home/RouterLink';
import { Icon } from '../shell/Icon';
import { basketKey, isConflict, isTooMany, loadBasket, patchBasket, stored } from './api';
import { BasketEditor } from './BasketEditor';
import { BasketResults } from './BasketResults';
import { setDeviceBasket, useDeviceBasket } from './device';
import { fromBasket, signature, toInput, type EditLine } from './model';
import { SaveDeviceOffer } from './SaveDeviceOffer';
import styles from './basket.module.css';

/** How long after the last change a saved basket writes (one PATCH per pause, not per tap). */
const SAVE_AFTER_MS = 600;

function Back() {
  const intl = useIntl();
  return (
    <RouterLink href="/baskets" className="tz-btn tz-btn-ghost tz-btn-sm">
      <Icon icon={ChevronLeft} />
      {intl.formatMessage({ id: 'basket.back' })}
    </RouterLink>
  );
}

export function SavedBasketPage({ basketId }: { basketId: string }) {
  const intl = useIntl();
  const query = useQuery({ queryKey: basketKey(basketId), queryFn: () => loadBasket(basketId), retry: false });
  return (
    <div className={styles.page}>
      <header className={`tz-hero-wash ${styles.head}`}>
        <Back />
        <h1 className={styles.title}>{query.data?.name ?? intl.formatMessage({ id: 'basket.title' })}</h1>
      </header>
      <div className={styles.body}>
        {query.isPending && <Skeleton shape="block" height="160px" />}
        {query.isError && (
          <EmptyState
            icon={ShoppingBasket}
            title={intl.formatMessage({ id: query.error instanceof ApiError && query.error.status === 404 ? 'basket.gone.title' : 'basket.error' })}
            action={
              <RouterLink href="/baskets" className="tz-btn tz-btn-primary">
                {intl.formatMessage({ id: 'basket.back' })}
              </RouterLink>
            }
          />
        )}
        {query.data && <SavedBasket basket={query.data} />}
      </div>
    </div>
  );
}

type Note = 'saving' | 'saved' | 'conflict' | 'full' | 'error' | null;

interface Draft {
  name: string | null;
  lines: EditLine[] | null;
}

function SavedBasket({ basket }: { basket: Basket }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const unitLabel = useUnitLabel();
  const unit = (size: number, u: string) => unitLabel(size, u as Parameters<typeof unitLabel>[1]);
  const [lines, setLines] = useState<EditLine[]>(() => fromBasket(basket, unit));
  // What has changed since the last write and not been sent yet.
  const [draft, setDraft] = useState<Draft>({ name: null, lines: null });
  const [note, setNote] = useState<Note>(null);

  // A newer version from the server (our own write, or a reload after 412) replaces the lines unless
  // the customer has changed them again meanwhile.
  const [seen, setSeen] = useState(basket.version);
  if (seen !== basket.version) {
    setSeen(basket.version);
    if (draft.lines === null) setLines(fromBasket(basket, unit));
  }

  const save = useMutation({
    mutationFn: (d: Draft) => {
      const latest = qc.getQueryData<Basket>(basketKey(basket.basketId)) ?? basket;
      const patch: BasketPatch = {};
      if (d.name !== null) patch.name = d.name;
      if (d.lines !== null) patch.lines = toInput(d.lines);
      return patchBasket(latest, patch);
    },
    onSuccess: (b, sent) => {
      setDraft((cur) => ({
        name: cur.name === sent.name ? null : cur.name,
        lines: cur.lines && sent.lines && signature(cur.lines) === signature(sent.lines) ? null : cur.lines,
      }));
      stored(qc, b);
      setNote('saved');
    },
    onError: (err) => {
      setDraft({ name: null, lines: null });
      if (isConflict(err)) {
        setNote('conflict');
        void qc.invalidateQueries({ queryKey: basketKey(basket.basketId) });
        return;
      }
      setNote(isTooMany(err) ? 'full' : 'error');
      setLines(fromBasket(qc.getQueryData<Basket>(basketKey(basket.basketId)) ?? basket, unit));
    },
  });

  const pending = draft.name !== null || draft.lines !== null;
  const { isPending, mutate } = save;
  useEffect(() => {
    if (!pending || isPending) return;
    const t = window.setTimeout(() => {
      setNote('saving');
      mutate(draft);
    }, SAVE_AFTER_MS);
    return () => window.clearTimeout(t);
  }, [pending, isPending, draft, mutate]);

  const onLines = (next: EditLine[]) => {
    setLines(next);
    setDraft((d) => ({ ...d, lines: next }));
  };

  // After a search on the saved lines, the split is kept with the basket (lastSplit, D-111).
  const onResult = (r: BasketSearchResult, searched: readonly EditLine[]) => {
    const latest = qc.getQueryData<Basket>(basketKey(basket.basketId)) ?? basket;
    const saved = fromBasket(latest, unit);
    if (pending || save.isPending || signature(saved) !== signature(searched)) return;
    const ids = (idx: number[]) => idx.map((i) => saved[i]?.key).filter((k): k is string => typeof k === 'string');
    const lastSplit = r.split
      ? {
          stores: r.split.stores.map((s) => ({ storeId: s.storeId, storeSlug: s.storeSlug, storeName: s.storeName, lineIds: ids(s.lineIndexes) })),
          uncoveredLineIds: ids(r.split.uncovered),
          searchedAt: r.serverNow,
        }
      : null;
    if (lastSplit === null && latest.lastSplit === null) return;
    patchBasket(latest, { lastSplit })
      .then((b) => stored(qc, b))
      .catch(() => undefined);
  };

  const status =
    note === 'saving'
      ? intl.formatMessage({ id: 'basket.saving' })
      : note === 'saved'
        ? intl.formatMessage({ id: 'basket.saved' })
        : undefined;

  const last = basket.lastSplit;
  return (
    <>
      {note === 'conflict' && (
        <Banner tone="info" live="status">
          {intl.formatMessage({ id: 'basket.conflict' })}
        </Banner>
      )}
      {note === 'full' && (
        <Banner tone="warning" live="status">
          {intl.formatMessage({ id: 'basket.full' })}
        </Banner>
      )}
      {note === 'error' && (
        <Banner tone="warning" live="status">
          {intl.formatMessage({ id: 'basket.saveError' })}
        </Banner>
      )}
      <BasketEditor
        name={basket.name}
        lines={lines}
        status={status}
        onLines={onLines}
        onName={(name) => setDraft((d) => ({ ...d, name }))}
      />
      {last && last.stores.length === 2 && (
        <p className="tz-meta">
          {intl.formatMessage({ id: 'basket.lastSplit' }, { a: last.stores[0]?.storeName ?? '', b: last.stores[1]?.storeName ?? '' })}
        </p>
      )}
      <BasketResults lines={lines} onResult={onResult} />
    </>
  );
}

export function DeviceBasketPage() {
  const intl = useIntl();
  const basket = useDeviceBasket();
  const { status } = useSessionState();
  return (
    <div className={styles.page}>
      <header className={`tz-hero-wash ${styles.head}`}>
        <Back />
        <h1 className={styles.title}>{basket.name || intl.formatMessage({ id: 'basket.device.title' })}</h1>
      </header>
      <div className={styles.body}>
        {status === 'signedOut' ? (
          <Banner
            tone="info"
            action={
              <RouterLink href="/sign-in?next=%2Fbaskets%2Fdevice" className="tz-btn tz-btn-soft tz-btn-sm">
                {intl.formatMessage({ id: 'basket.signIn' })}
              </RouterLink>
            }
          >
            {intl.formatMessage({ id: 'basket.device.note' })}
          </Banner>
        ) : status === 'signedIn' ? (
          <SaveDeviceOffer />
        ) : null}
        <BasketEditor
          name={basket.name}
          lines={basket.lines}
          onLines={(lines) => setDeviceBasket({ ...basket, lines })}
          onName={(name) => setDeviceBasket({ ...basket, name })}
        />
        <BasketResults lines={basket.lines} />
        {basket.lines.length > 0 && (
          <Button variant="ghost" size="sm" onClick={() => setDeviceBasket({ name: '', lines: [] })}>
            {intl.formatMessage({ id: 'basket.device.clear' })}
          </Button>
        )}
      </div>
    </div>
  );
}
