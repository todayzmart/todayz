// B-20 reply sheet: Yes (price prefilled from the listed price, locked for a cashier, never above the
// MRP; "You have {n}" when stock is short; optional ready-by) or No. On an item the shop does not list,
// owner and manager get "Add it to your stock" (B-07 add form, prefilled).
import type { StoreAsk } from '@todayz/api-client/gen/model/storeAsk';
import type { AskReplyCreate } from '@todayz/api-client/gen/model/askReplyCreate';
import { formatMoney } from '@todayz/i18n';
import { Button, Segmented, Sheet, TextField } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { parseRupees, rupeesText } from '../stock/format';
import type { CurrentStore } from '../orders/store';
import { askErrorKey, sendReply, storedAsk } from './api';
import { canAddItem, canEditAskPrice } from './perm';
import { readyByIso, readyByPast, readyDays, type ReadyDay } from './time';
import styles from './asks.module.css';

export interface ReplySheetProps {
  store: CurrentStore;
  ask: StoreAsk;
  /** Server clock minus device clock, from the list's serverNow. */
  offsetMs: number;
  onClose: () => void;
}

export function ReplySheet({ store, ask, offsetMs, onClose }: ReplySheetProps) {
  const intl = useIntl();
  const qc = useQueryClient();
  const listed = ask.storeItemId !== null && ask.listedPricePaise !== null;
  const editable = canEditAskPrice(store.role);
  const [price, setPrice] = useState(() => (ask.listedPricePaise !== null ? rupeesText(ask.listedPricePaise) : ''));
  const days = readyDays(ask.needBy);
  const [day, setDay] = useState<ReadyDay>('today');
  const [time, setTime] = useState('');
  const [busy, setBusy] = useState<'yes' | 'no' | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [fieldError, setFieldError] = useState<string | null>(null);

  const send = (body: AskReplyCreate) => {
    setBusy(body.answer);
    setError(null);
    sendReply(store, ask.askId, body)
      .then((a) => {
        storedAsk(qc, store.sid, a);
        onClose();
      })
      .catch((err: unknown) => setError(err))
      .finally(() => setBusy(null));
  };

  const onYes = () => {
    setFieldError(null);
    const body: AskReplyCreate = { answer: 'yes' };
    if (editable && price.trim() !== '') {
      const paise = parseRupees(price);
      if (paise === null) return setFieldError(intl.formatMessage({ id: 'asks.reply.priceInvalid' }));
      if (ask.mrpPaise !== null && paise > ask.mrpPaise) {
        return setFieldError(intl.formatMessage({ id: 'asks.reply.aboveMrp' }, { mrp: formatMoney(ask.mrpPaise) }));
      }
      if (!listed || paise !== ask.listedPricePaise) body.pricePaise = paise;
    }
    if (time !== '') {
      const nowMs = Date.now() + offsetMs;
      const iso = readyByIso(nowMs, day, time);
      if (iso === null || readyByPast(iso, nowMs)) return setFieldError(intl.formatMessage({ id: 'asks.reply.readyPast' }));
      body.readyBy = iso;
    }
    send(body);
  };

  const short = ask.available !== null && ask.available < ask.quantity;
  const addSearch = ask.productId ? { add: ask.productId } : { add_q: ask.itemName };

  return (
    <Sheet
      open
      onClose={onClose}
      title={ask.itemName}
      description={intl.formatMessage({ id: 'asks.reply.wants' }, { n: ask.quantity })}
      closeLabel={intl.formatMessage({ id: 'asks.sheet.close' })}
      footer={
        <>
          <Button loading={busy === 'no'} disabled={busy !== null} onClick={() => send({ answer: 'no' })}>
            {intl.formatMessage({ id: 'asks.reply.no' })}
          </Button>
          <Button variant="primary" loading={busy === 'yes'} disabled={busy !== null} onClick={onYes}>
            {intl.formatMessage({ id: 'asks.reply.yes' })}
          </Button>
        </>
      }
    >
      <div className={styles.sheetBody}>
        {editable ? (
          <TextField
            label={intl.formatMessage({ id: 'asks.reply.price' })}
            help={
              ask.mrpPaise !== null
                ? intl.formatMessage({ id: 'asks.reply.mrp' }, { mrp: formatMoney(ask.mrpPaise) })
                : listed
                  ? undefined
                  : intl.formatMessage({ id: 'asks.reply.priceOptional' })
            }
            prefix="₹"
            inputMode="decimal"
            value={price}
            onChange={(e) => setPrice(e.target.value)}
          />
        ) : listed && ask.listedPricePaise !== null ? (
          <div className={styles.locked}>
            <span className={styles.label}>{intl.formatMessage({ id: 'asks.reply.price' })}</span>
            <span className="tz-price">{formatMoney(ask.listedPricePaise)}</span>
            <span className="tz-meta">{intl.formatMessage({ id: 'asks.reply.priceLocked' })}</span>
          </div>
        ) : (
          <p className="tz-meta">{intl.formatMessage({ id: 'asks.reply.noPriceCashier' })}</p>
        )}

        {short && <p className={styles.short}>{intl.formatMessage({ id: 'asks.reply.youHave' }, { n: Math.max(0, ask.available ?? 0) })}</p>}

        <fieldset className={styles.ready}>
          <legend className={styles.label}>{intl.formatMessage({ id: 'asks.reply.readyBy' })}</legend>
          {days.length > 1 && (
            <Segmented
              label={intl.formatMessage({ id: 'asks.reply.readyDay' })}
              options={days.map((d) => ({ value: d, label: intl.formatMessage({ id: `asks.reply.day.${d}` }) }))}
              value={day}
              onChange={setDay}
            />
          )}
          <TextField
            label={intl.formatMessage({ id: 'asks.reply.readyTime' })}
            help={intl.formatMessage({ id: 'asks.reply.readyHelp' })}
            type="time"
            value={time}
            onChange={(e) => setTime(e.target.value)}
          />
        </fieldset>

        {ask.storeItemId === null && canAddItem(store.role) && (
          <Link to="/stock" search={addSearch} className="tz-btn tz-btn-soft tz-btn-sm">
            {intl.formatMessage({ id: 'asks.reply.addStock' })}
          </Link>
        )}

        {fieldError && (
          <p role="alert" className="tz-field-error">
            {fieldError}
          </p>
        )}
        {error !== null && (
          <p role="alert" className="tz-field-error">
            {intl.formatMessage({ id: `asks.error.${askErrorKey(error)}` })}
          </p>
        )}
      </div>
    </Sheet>
  );
}
