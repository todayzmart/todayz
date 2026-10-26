import { ApiError } from '@todayz/api-client';
import { newUlid } from '@todayz/auth';
import { Banner, Button, Chip, Segmented, Sheet, TextField, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { useMemo, useRef, useState, type ReactNode } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { dateText } from '../stock/labels';
import { useCategories, useItems } from '../stock/queries';
import { emptyDraft, scopeOf, validateDraft, type FieldError, type FieldKey, type OfferChoice, type OfferDraft, type PickedItem } from './form';
import { ItemPicker } from './ItemPicker';
import { OptionsGate } from './OptionsGate';
import { OFFER_LIMITS } from './limits';
import { OfferPreview } from './OfferPreview';
import { sampleLines } from './preview';
import { createOffers, offersErrorKey, useExpiringBatches, usePreview } from './queries';
import styles from './offers.module.css';

const CHOICES: readonly OfferChoice[] = ['percent', 'flat', 'bxgy', 'bill_min', 'flash'];

export interface OfferSheetProps {
  store: CurrentStore;
  /** A deep link (D-99) or a nudge prefills the form; it never creates anything. */
  initial?: Partial<OfferDraft> | undefined;
  onClose: () => void;
}

/** B-08 "New offer": every kind in the plan, a live preview priced by the server, and nothing saved until Create. */
export function OfferSheet({ store, initial, onClose }: OfferSheetProps) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const [d, setD] = useState<OfferDraft>(() => emptyDraft(initial));
  const [showErrors, setShowErrors] = useState(false);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>(null);
  const keys = useRef(new Map<string, string>());
  const set = <K extends keyof OfferDraft>(k: K, v: OfferDraft[K]) => setD((x) => ({ ...x, [k]: v }));

  const batches = useExpiringBatches(store, d.choice === 'flash');
  const categories = useCategories();
  const scope = scopeOf(d);
  const pool = useItems(store, scope === 'category' && d.categoryId ? { categoryId: d.categoryId, listed: true } : { listed: true });
  const poolItems = useMemo<PickedItem[]>(
    () => (pool.data?.pages.flatMap((p) => p.items) ?? []).slice(0, 20).map((i) => ({ id: i.id, title: i.name, pricePaise: i.pricePaise })),
    [pool.data],
  );

  const [openedAt] = useState(() => Date.now());
  const check = useMemo(() => validateDraft(d, { batches: batches.data ?? [], now: openedAt }), [d, batches.data, openedAt]);
  // The preview prices the first valid body; a flash form shows its first batch.
  const body = check.bodies[0] ?? null;
  const lines = useMemo(() => (body ? sampleLines(body, d.items, poolItems) : []), [body, d.items, poolItems]);
  const preview = usePreview(store, body, lines);
  const names = useMemo(() => {
    const m = new Map<string, string>();
    for (const i of [...poolItems, ...d.items]) m.set(i.id, i.title);
    for (const b of batches.data ?? []) m.set(b.itemId, b.itemName);
    return m;
  }, [poolItems, d.items, batches.data]);

  const err = (k: FieldKey): ReactNode => {
    const e: FieldError | undefined = showErrors ? check.errors[k] : undefined;
    return e ? intl.formatMessage({ id: `offers.err.${e.code}` }, e.values) : undefined;
  };

  const submit = async () => {
    setShowErrors(true);
    // Checked again with the clock now: the sheet may have been open a while.
    const final = validateDraft(d, { batches: batches.data ?? [], now: Date.now() });
    if (final.bodies.length === 0) return;
    setBusy(true);
    setFailure(null);
    const ids = final.bodies.map((b) => {
      const sig = JSON.stringify(b);
      const key = keys.current.get(sig) ?? newUlid();
      keys.current.set(sig, key);
      return key;
    });
    const out = await createOffers(qc, store, final.bodies, ids);
    setBusy(false);
    if (out.failed.length === 0) {
      keys.current.clear();
      toast.show({ message: intl.formatMessage({ id: 'offers.created' }, { count: final.bodies.length }) });
      onClose();
      return;
    }
    // The ones that went through are done; a retry sends only the rest, and a server answer retires its key.
    const failedBatches = out.failed.flatMap((b) => (b.batchId ? [b.batchId] : []));
    if (out.error instanceof ApiError && out.failed[0]) keys.current.delete(JSON.stringify(out.failed[0]));
    if (d.choice === 'flash' && failedBatches.length < final.bodies.length) set('batchIds', failedBatches);
    setFailure(out.error);
  };

  const flat = (categories.data?.items ?? []).flatMap((c) => [{ id: c.id, name: c.name }, ...c.children.map((k) => ({ id: k.id, name: `${c.name} · ${k.name}` }))]);
  const money = d.choice === 'flat' || d.choice === 'bill_min';

  return (
    <Sheet open onClose={onClose} title={intl.formatMessage({ id: 'offers.new.title' })} closeLabel={intl.formatMessage({ id: 'offers.close' })}>
      <form
        className={styles.form}
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <div className={styles.chips} role="group" aria-label={intl.formatMessage({ id: 'offers.kind.label' })}>
          {CHOICES.map((c) => (
            <Chip key={c} pressed={d.choice === c} onPressedChange={() => set('choice', c)}>
              {intl.formatMessage({ id: `offers.kind.${c}` })}
            </Chip>
          ))}
        </div>

        {(d.choice === 'percent' || d.choice === 'flat') && (
          <Segmented
            label={intl.formatMessage({ id: 'offers.scope.label' })}
            value={d.scope}
            onChange={(v) => set('scope', v)}
            options={[
              { value: 'items', label: intl.formatMessage({ id: 'offers.scope.items' }) },
              { value: 'category', label: intl.formatMessage({ id: 'offers.scope.category' }) },
              { value: 'store', label: intl.formatMessage({ id: 'offers.scope.store' }) },
            ]}
          />
        )}

        {d.choice === 'flash' ? (
          <fieldset className={styles.group}>
            <legend className="tz-field-label">{intl.formatMessage({ id: 'offers.flash.batches' })}</legend>
            <OptionsGate query={batches} loadingId="app.loading" failedId="offers.error.body" height="5rem">
              {(batches.data ?? []).length === 0 ? (
                <p className={styles.note}>{intl.formatMessage({ id: 'offers.flash.none' })}</p>
              ) : (
                (batches.data ?? []).map((b) => (
                  <label key={b.id} className={styles.check}>
                    <input
                      type="checkbox"
                      checked={d.batchIds.includes(b.id)}
                      onChange={(e) => set('batchIds', e.target.checked ? [...d.batchIds, b.id] : d.batchIds.filter((x) => x !== b.id))}
                    />
                    <span className={styles.grow}>{b.itemName}</span>
                    <span className={styles.meta}>
                      {intl.formatMessage({ id: 'offers.flash.batchMeta' }, { qty: b.quantity, date: b.expiresOn ? dateText(intl, b.expiresOn) : '' })}
                    </span>
                  </label>
                ))
              )}
            </OptionsGate>
            {err('batches') && <p className="tz-field-error">{err('batches')}</p>}
            <p className={styles.note}>{intl.formatMessage({ id: 'offers.flash.help' })}</p>
          </fieldset>
        ) : scope === 'items' ? (
          <fieldset className={styles.group}>
            <legend className="tz-field-label">{intl.formatMessage({ id: d.choice === 'bxgy' ? 'offers.items.one' : 'offers.items.many' })}</legend>
            <ItemPicker store={store} picked={d.items} max={d.choice === 'bxgy' ? 1 : OFFER_LIMITS.maxItems} onChange={(items) => set('items', items)} />
            {err('items') && <p className="tz-field-error">{err('items')}</p>}
          </fieldset>
        ) : scope === 'category' ? (
          <div className="tz-field">
            <label className="tz-field-label" htmlFor="offer-category">
              {intl.formatMessage({ id: 'offers.category.label' })}
            </label>
            <OptionsGate query={categories} loadingId="app.loading" failedId="offers.error.body">
              <select id="offer-category" className="tz-input" value={d.categoryId} onChange={(e) => set('categoryId', e.target.value)}>
                <option value="">{intl.formatMessage({ id: 'offers.category.choose' })}</option>
                {flat.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
            </OptionsGate>
            {err('category') && <p className="tz-field-error">{err('category')}</p>}
          </div>
        ) : (
          <p className={styles.note}>{intl.formatMessage({ id: 'offers.scope.storeNote' })}</p>
        )}

        {d.choice !== 'bxgy' && (
          <TextField
            label={intl.formatMessage({ id: money ? (d.choice === 'bill_min' ? 'offers.value.billOff' : 'offers.value.flatOff') : 'offers.value.percent' })}
            help={intl.formatMessage({ id: d.choice === 'flat' ? 'offers.value.flatHelp' : d.choice === 'bill_min' ? 'offers.value.billHelp' : 'offers.value.percentHelp' }, { max: OFFER_LIMITS.percentMax })}
            error={err('value')}
            inputMode={money ? 'decimal' : 'numeric'}
            prefix={money ? '₹' : undefined}
            value={d.value}
            onChange={(e) => set('value', e.target.value)}
          />
        )}
        {d.choice === 'bill_min' && (
          <TextField
            label={intl.formatMessage({ id: 'offers.minBill.label' })}
            error={err('minBill')}
            inputMode="decimal"
            prefix="₹"
            value={d.minBill}
            onChange={(e) => set('minBill', e.target.value)}
          />
        )}
        {d.choice === 'bxgy' && (
          <div className={styles.pair}>
            <TextField label={intl.formatMessage({ id: 'offers.buy.label' })} error={err('buy')} inputMode="numeric" value={d.buyQty} onChange={(e) => set('buyQty', e.target.value)} />
            <TextField label={intl.formatMessage({ id: 'offers.get.label' })} error={err('get')} inputMode="numeric" value={d.getQty} onChange={(e) => set('getQty', e.target.value)} />
          </div>
        )}

        <TextField label={intl.formatMessage({ id: 'offers.title.label' })} help={intl.formatMessage({ id: 'offers.title.help' })} error={err('title')} value={d.title} onChange={(e) => set('title', e.target.value)} />

        <Segmented
          label={intl.formatMessage({ id: 'offers.channels.label' })}
          value={d.channels}
          onChange={(v) => set('channels', v)}
          options={[
            { value: 'both', label: intl.formatMessage({ id: 'offers.channels.both' }) },
            { value: 'online', label: intl.formatMessage({ id: 'offers.channels.online' }) },
            { value: 'pos', label: intl.formatMessage({ id: 'offers.channels.pos' }) },
          ]}
        />

        <label className={styles.check}>
          <input type="checkbox" checked={d.oncePerCustomer} onChange={(e) => set('oncePerCustomer', e.target.checked)} />
          <span className={styles.grow}>{intl.formatMessage({ id: 'offers.once.label' })}</span>
        </label>
        <TextField label={intl.formatMessage({ id: 'offers.max.label' })} help={intl.formatMessage({ id: 'offers.max.help' })} error={err('maxRedemptions')} inputMode="numeric" value={d.maxRedemptions} onChange={(e) => set('maxRedemptions', e.target.value)} />

        {d.choice !== 'flash' && (
          <div className={styles.pair}>
            <TextField label={intl.formatMessage({ id: 'offers.starts.label' })} help={intl.formatMessage({ id: 'offers.starts.help' })} error={err('startsAt')} type="datetime-local" value={d.startsAt} onChange={(e) => set('startsAt', e.target.value)} />
            <TextField label={intl.formatMessage({ id: 'offers.ends.label' })} help={intl.formatMessage({ id: 'offers.ends.help' })} error={err('endsAt')} type="datetime-local" value={d.endsAt} onChange={(e) => set('endsAt', e.target.value)} />
          </div>
        )}

        <OfferPreview waiting={body === null || lines.length === 0} pending={preview.isPending || preview.isFetching} failed={preview.isError} preview={preview.data} names={names} />

        {failure !== null && (
          <Banner tone="warning">
            <span role="alert">{intl.formatMessage({ id: `offers.error.${offersErrorKey(failure)}` })}</span>
          </Banner>
        )}
        <div className={styles.actions}>
          <Button onClick={onClose}>{intl.formatMessage({ id: 'offers.cancel' })}</Button>
          <Button variant="primary" type="submit" loading={busy}>
            {intl.formatMessage({ id: 'offers.create' })}
          </Button>
        </div>
      </form>
    </Sheet>
  );
}
