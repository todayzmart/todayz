import type { MovementCreate } from '@todayz/api-client/gen/model/movementCreate';
import type { StockBatch } from '@todayz/api-client/gen/model/stockBatch';
import type { StoreItem } from '@todayz/api-client/gen/model/storeItem';
import { formatMoney } from '@todayz/i18n';
import { Button, TextField, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { useIdempotentAction } from './action';
import { planExpiry } from './deeplink';
import { itemTitle, parseQuantity, parseRupees } from './format';
import { dateText } from './labels';
import { sendMovement } from './queries';
import { StockError } from './StockError';
import styles from './stock.module.css';

export type MovementFormKind = 'restock' | 'damage' | 'expired' | 'count_correction';

export interface MovementFormProps {
  store: CurrentStore;
  item: StoreItem;
  kind: MovementFormKind;
  /** The item's batches (for "Mark expired"). */
  batches?: readonly StockBatch[] | undefined;
  /** Prefill from a deep link: the quantity (restock) or the batches ticked (expire). */
  initialQuantity?: number | undefined;
  initialBatchIds?: readonly string[] | undefined;
  onDone: () => void;
  onCancel: () => void;
}

/**
 * The four stock writes on one item: restock, damage, expired and a shelf count. Nothing is sent
 * until Save; a prefilled form (insight deep link) is only a starting point.
 */
export function MovementForm({ store, item, kind, batches, initialQuantity, initialBatchIds, onDone, onCancel }: MovementFormProps) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const action = useIdempotentAction();
  const live = (batches ?? []).filter((b) => b.quantity > 0);
  const byBatch = kind === 'expired' && live.length > 0;
  const [qty, setQty] = useState(initialQuantity !== undefined ? String(initialQuantity) : kind === 'count_correction' ? String(item.onHand) : '');
  const [expiresOn, setExpiresOn] = useState('');
  const [cost, setCost] = useState('');
  const [note, setNote] = useState('');
  const [ticked, setTicked] = useState<string[]>(() =>
    byBatch && initialBatchIds ? planExpiry(live, initialBatchIds, item.available).map((p) => p.id) : [],
  );
  const [invalid, setInvalid] = useState<string | null>(null);

  const plan = planExpiry(live, ticked, item.available);
  const planTotal = plan.reduce((n, p) => n + p.take, 0);
  const title = itemTitle(item);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const body: MovementCreate = { kind };
    const trimmedNote = note.trim();
    if (byBatch) {
      if (ticked.length === 0) return setInvalid('stock.form.pickBatch');
      body.batchIds = live.filter((b) => ticked.includes(b.id)).map((b) => b.id);
    } else {
      const n = parseQuantity(qty);
      const min = kind === 'count_correction' ? 0 : 1;
      if (n === null || n < min) return setInvalid(kind === 'count_correction' ? 'stock.form.countInvalid' : 'stock.form.qtyInvalid');
      if ((kind === 'damage' || kind === 'expired') && n > item.available) return setInvalid('stock.form.qtyTooMany');
      body.quantity = n;
    }
    if (kind === 'restock') {
      if (expiresOn) body.expiresOn = expiresOn;
      if (cost.trim()) {
        const c = parseRupees(cost);
        if (c === null) return setInvalid('stock.form.costInvalid');
        body.costPaise = c;
      }
    }
    if (trimmedNote) body.note = trimmedNote;
    setInvalid(null);
    const out = await action.run(JSON.stringify({ item: item.id, ...body }), (key) => sendMovement(qc, store, item.id, body, key));
    if (out) {
      toast.show({ message: intl.formatMessage({ id: `stock.toast.${kind}` }, { item: title }) });
      onDone();
    }
  };

  const qtyError = invalid && invalid !== 'stock.form.pickBatch' && invalid !== 'stock.form.costInvalid' ? intl.formatMessage({ id: invalid }, { available: item.available }) : undefined;

  return (
    <form className={styles.form} onSubmit={(e) => void submit(e)} noValidate aria-label={intl.formatMessage({ id: `stock.form.title.${kind}` }, { item: title })}>
      <h3 className={styles.formTitle}>{intl.formatMessage({ id: `stock.form.title.${kind}` }, { item: title })}</h3>
      {byBatch ? (
        <fieldset className={styles.batchPick}>
          <legend className="tz-field-label">{intl.formatMessage({ id: 'stock.form.batches' })}</legend>
          {live.map((b) => (
            <label key={b.id} className={styles.pick}>
              <input
                type="checkbox"
                checked={ticked.includes(b.id)}
                onChange={(e) => setTicked((t) => (e.target.checked ? [...t, b.id] : t.filter((x) => x !== b.id)))}
              />
              <span>
                {b.expiresOn ? intl.formatMessage({ id: 'stock.batch.expires' }, { date: dateText(intl, b.expiresOn) }) : intl.formatMessage({ id: 'stock.batch.noExpiry' })}
                {' · '}
                {intl.formatMessage({ id: 'stock.batch.left' }, { quantity: b.quantity })}
              </span>
            </label>
          ))}
          <p className="tz-field-help">{intl.formatMessage({ id: 'stock.form.writeOff' }, { quantity: planTotal })}</p>
          {invalid === 'stock.form.pickBatch' && (
            <p role="alert" className="tz-field-error">
              {intl.formatMessage({ id: 'stock.form.pickBatch' })}
            </p>
          )}
        </fieldset>
      ) : (
        <TextField
          label={intl.formatMessage({ id: kind === 'count_correction' ? 'stock.form.counted' : 'stock.form.qty' })}
          help={
            kind === 'count_correction'
              ? intl.formatMessage({ id: 'stock.form.countedHelp' }, { onHand: item.onHand, reserved: item.reserved })
              : kind === 'restock'
                ? undefined
                : intl.formatMessage({ id: 'stock.form.qtyMax' }, { available: item.available })
          }
          error={qtyError}
          inputMode="numeric"
          value={qty}
          onChange={(e) => setQty(e.target.value)}
        />
      )}
      {kind === 'restock' && (
        <>
          <TextField
            label={intl.formatMessage({ id: 'stock.form.expiry' })}
            help={intl.formatMessage({ id: 'stock.form.expiryHelp' })}
            type="date"
            value={expiresOn}
            onChange={(e) => setExpiresOn(e.target.value)}
          />
          <TextField
            label={intl.formatMessage({ id: 'stock.form.cost' })}
            help={intl.formatMessage({ id: 'stock.form.costHelp' }, { price: formatMoney(item.pricePaise, 'product') })}
            error={invalid === 'stock.form.costInvalid' ? intl.formatMessage({ id: 'stock.form.costInvalid' }) : undefined}
            inputMode="decimal"
            prefix="₹"
            value={cost}
            onChange={(e) => setCost(e.target.value)}
          />
        </>
      )}
      <TextField label={intl.formatMessage({ id: 'stock.form.note' })} maxLength={200} value={note} onChange={(e) => setNote(e.target.value)} />
      <StockError error={action.error} />
      <div className={styles.formActions}>
        <Button onClick={onCancel}>{intl.formatMessage({ id: 'stock.form.cancel' })}</Button>
        <Button type="submit" variant="primary" loading={action.pending}>
          {intl.formatMessage({ id: `stock.form.save.${kind}` })}
        </Button>
      </div>
    </form>
  );
}
