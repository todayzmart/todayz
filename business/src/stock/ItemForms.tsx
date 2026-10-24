import type { StockBatch } from '@todayz/api-client/gen/model/stockBatch';
import type { StoreItem } from '@todayz/api-client/gen/model/storeItem';
import { useSession } from '@todayz/auth';
import { formatMoney } from '@todayz/i18n';
import { Button, TextField, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { useIdempotentAction } from './action';
import { effectiveMrp, itemTitle, parseQuantity, parseRupees, rupeesText } from './format';
import { patchBatch, patchItem } from './queries';
import { StockError } from './StockError';
import styles from './stock.module.css';

interface FormBase {
  store: CurrentStore;
  onDone: () => void;
  onCancel: () => void;
}

/** The selling price, checked against the MRP here and by the server (a price above MRP is a 422). */
export function PriceForm({ store, item, reference, onDone, onCancel }: FormBase & { item: StoreItem; reference?: number | undefined }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const session = useSession();
  const toast = useToast();
  const action = useIdempotentAction();
  const [price, setPrice] = useState(rupeesText(item.pricePaise));
  const [invalid, setInvalid] = useState<string | null>(null);
  const mrp = effectiveMrp(item);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const paise = parseRupees(price);
    if (paise === null) return setInvalid('stock.form.priceInvalid');
    if (mrp !== null && paise > mrp) return setInvalid('stock.form.priceAboveMrp');
    setInvalid(null);
    const out = await action.run(`price:${paise}`, () => patchItem(qc, session, store, item.id, { pricePaise: paise }));
    if (out) {
      toast.show({ message: intl.formatMessage({ id: 'stock.toast.price' }, { item: itemTitle(item) }) });
      onDone();
    }
  };

  return (
    <form className={styles.form} onSubmit={(e) => void submit(e)} noValidate aria-label={intl.formatMessage({ id: 'stock.form.title.price' })}>
      <h3 className={styles.formTitle}>{intl.formatMessage({ id: 'stock.form.title.price' }, { item: itemTitle(item) })}</h3>
      {reference !== undefined && (
        <p className={styles.note}>{intl.formatMessage({ id: 'stock.form.priceRef' }, { price: formatMoney(reference, 'product') })}</p>
      )}
      <TextField
        label={intl.formatMessage({ id: 'stock.form.price' })}
        help={mrp !== null ? intl.formatMessage({ id: 'stock.form.priceMrp' }, { mrp: formatMoney(mrp, 'product') }) : undefined}
        error={invalid ? intl.formatMessage({ id: invalid }, { mrp: mrp === null ? '' : formatMoney(mrp, 'product') }) : undefined}
        inputMode="decimal"
        prefix="₹"
        value={price}
        onChange={(e) => setPrice(e.target.value)}
      />
      <StockError error={action.error} />
      <div className={styles.formActions}>
        <Button onClick={onCancel}>{intl.formatMessage({ id: 'stock.form.cancel' })}</Button>
        <Button type="submit" variant="primary" loading={action.pending}>
          {intl.formatMessage({ id: 'stock.form.save.price' })}
        </Button>
      </div>
    </form>
  );
}

/** The low-stock threshold; empty clears it. */
export function ThresholdForm({ store, item, onDone, onCancel }: FormBase & { item: StoreItem }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const session = useSession();
  const toast = useToast();
  const action = useIdempotentAction();
  const [value, setValue] = useState(item.lowStockAt === null ? '' : String(item.lowStockAt));
  const [invalid, setInvalid] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const n = value.trim() === '' ? null : parseQuantity(value);
    if (value.trim() !== '' && n === null) return setInvalid(true);
    setInvalid(false);
    const out = await action.run(`low:${String(n)}`, () => patchItem(qc, session, store, item.id, { lowStockAt: n }));
    if (out) {
      toast.show({ message: intl.formatMessage({ id: 'stock.toast.threshold' }, { item: itemTitle(item) }) });
      onDone();
    }
  };

  return (
    <form className={styles.form} onSubmit={(e) => void submit(e)} noValidate aria-label={intl.formatMessage({ id: 'stock.form.title.threshold' })}>
      <h3 className={styles.formTitle}>{intl.formatMessage({ id: 'stock.form.title.threshold' })}</h3>
      <TextField
        label={intl.formatMessage({ id: 'stock.form.threshold' })}
        help={intl.formatMessage({ id: 'stock.form.thresholdHelp' })}
        error={invalid ? intl.formatMessage({ id: 'stock.form.qtyInvalid' }) : undefined}
        inputMode="numeric"
        value={value}
        onChange={(e) => setValue(e.target.value)}
      />
      <StockError error={action.error} />
      <div className={styles.formActions}>
        <Button onClick={onCancel}>{intl.formatMessage({ id: 'stock.form.cancel' })}</Button>
        <Button type="submit" variant="primary" loading={action.pending}>
          {intl.formatMessage({ id: 'stock.form.save.threshold' })}
        </Button>
      </div>
    </form>
  );
}

/** Cost and expiry of one batch ("Check cost" deep link lands here). */
export function BatchForm({ store, batch, onDone, onCancel }: FormBase & { batch: StockBatch }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const action = useIdempotentAction();
  const [cost, setCost] = useState(batch.costPaise === null ? '' : rupeesText(batch.costPaise));
  const [expiresOn, setExpiresOn] = useState(batch.expiresOn ?? '');
  const [invalid, setInvalid] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const costPaise = cost.trim() === '' ? null : parseRupees(cost);
    if (cost.trim() !== '' && costPaise === null) return setInvalid(true);
    setInvalid(false);
    const body = { costPaise, expiresOn: expiresOn === '' ? null : expiresOn };
    const out = await action.run(JSON.stringify(body), () => patchBatch(qc, store, batch, body));
    if (out) {
      toast.show({ message: intl.formatMessage({ id: 'stock.toast.batch' }) });
      onDone();
    }
  };

  return (
    <form className={styles.form} onSubmit={(e) => void submit(e)} noValidate aria-label={intl.formatMessage({ id: 'stock.form.title.batch' })}>
      <h3 className={styles.formTitle}>{intl.formatMessage({ id: 'stock.form.title.batch' })}</h3>
      <TextField
        label={intl.formatMessage({ id: 'stock.form.cost' })}
        help={intl.formatMessage({ id: 'stock.form.costHelp' }, { price: formatMoney(batch.pricePaise, 'product') })}
        error={invalid ? intl.formatMessage({ id: 'stock.form.costInvalid' }) : undefined}
        inputMode="decimal"
        prefix="₹"
        value={cost}
        onChange={(e) => setCost(e.target.value)}
      />
      <TextField
        label={intl.formatMessage({ id: 'stock.form.expiry' })}
        type="date"
        value={expiresOn}
        onChange={(e) => setExpiresOn(e.target.value)}
      />
      <StockError error={action.error} />
      <div className={styles.formActions}>
        <Button onClick={onCancel}>{intl.formatMessage({ id: 'stock.form.cancel' })}</Button>
        <Button type="submit" variant="primary" loading={action.pending}>
          {intl.formatMessage({ id: 'stock.form.save.batch' })}
        </Button>
      </div>
    </form>
  );
}
