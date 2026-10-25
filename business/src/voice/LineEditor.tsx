import type { StoreItem } from '@todayz/api-client/gen/model/storeItem';
import type { VoiceStockLine } from '@todayz/api-client/gen/model/voiceStockLine';
import type { VoiceStockLineKind } from '@todayz/api-client/gen/model/voiceStockLineKind';
import { formatMoney } from '@todayz/i18n';
import { Button, SearchField, Segmented, Sheet, Skeleton, TextField } from '@todayz/ui';
import { useMemo, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { effectiveMrp, itemTitle, parseRupees, rupeesText } from '../stock/format';
import { useItems } from '../stock/queries';
import { canConfirmKind, effective, KINDS, MAX_QUANTITY, numberOk, type Choice } from './lines';
import styles from './voice.module.css';

export interface LineEditorProps {
  store: CurrentStore;
  line: VoiceStockLine;
  choice: Choice;
  onSave: (c: Extract<Choice, { outcome: 'edited' }>) => void;
  onClose: () => void;
}

interface Picked {
  id: string;
  name: string;
  mrpPaise: number | null;
}

/**
 * Edit one line before Confirm: the item (search the shop's items; an unmatched line starts here),
 * the kind and the number. Nothing is sent until the draft is confirmed.
 */
export function LineEditor({ store, line, choice, onSave, onClose }: LineEditorProps) {
  const intl = useIntl();
  const start = effective(line, choice);
  const [kind, setKind] = useState<VoiceStockLineKind>(start.kind);
  const [item, setItem] = useState<Picked | null>(start.storeItemId ? { id: start.storeItemId, name: start.itemName ?? '', mrpPaise: null } : null);
  const [picking, setPicking] = useState(start.storeItemId === null);
  const [qty, setQty] = useState(start.quantity !== null ? String(start.quantity) : '');
  const [price, setPrice] = useState(start.pricePaise !== null ? rupeesText(start.pricePaise) : '');
  const [tried, setTried] = useState(false);

  const quantity = /^\d{1,4}$/.test(qty.trim()) ? Number(qty.trim()) : null;
  const pricePaise = parseRupees(price);
  const value = { kind, quantity: kind === 'set_price' ? null : quantity, pricePaise: kind === 'set_price' ? pricePaise : null };
  const mrp = item?.mrpPaise ?? null;
  const aboveMrp = kind === 'set_price' && mrp !== null && pricePaise !== null && pricePaise > mrp;
  const ok = item !== null && numberOk(value) && !aboveMrp;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTried(true);
    if (!ok || !item) return;
    onSave({ outcome: 'edited', kind, storeItemId: item.id, itemName: item.name, quantity: value.quantity, pricePaise: value.pricePaise });
  };

  const numberError = (() => {
    if (!tried) return undefined;
    if (kind === 'set_price') {
      if (pricePaise === null) return intl.formatMessage({ id: 'voice.edit.priceInvalid' });
      if (aboveMrp && mrp !== null) return intl.formatMessage({ id: 'voice.edit.aboveMrp' }, { mrp: formatMoney(mrp) });
      return undefined;
    }
    return numberOk(value) ? undefined : intl.formatMessage({ id: kind === 'count' ? 'voice.edit.countInvalid' : 'voice.edit.qtyInvalid' }, { max: MAX_QUANTITY });
  })();

  return (
    <Sheet
      open
      onClose={onClose}
      title={intl.formatMessage({ id: line.storeItemId === null && choice.outcome !== 'edited' ? 'voice.edit.pickTitle' : 'voice.edit.title' })}
      description={intl.formatMessage({ id: 'voice.line.heard' }, { words: line.heard })}
      closeLabel={intl.formatMessage({ id: 'voice.edit.close' })}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {intl.formatMessage({ id: 'voice.edit.cancel' })}
          </Button>
          <Button variant="primary" type="submit" form="voice-line-edit">
            {intl.formatMessage({ id: 'voice.edit.save' })}
          </Button>
        </>
      }
    >
      <form id="voice-line-edit" className={styles.form} onSubmit={submit} noValidate>
        <div className={styles.field}>
          <span className={styles.label}>{intl.formatMessage({ id: 'voice.edit.item' })}</span>
          {item && !picking ? (
            <div className={styles.pickedRow}>
              <span className={styles.grow}>{item.name}</span>
              <Button size="sm" onClick={() => setPicking(true)}>
                {intl.formatMessage({ id: 'voice.edit.changeItem' })}
              </Button>
            </div>
          ) : (
            <ItemSearch
              store={store}
              initial={line.storeItemId === null ? line.itemName ?? '' : ''}
              onPick={(i) => {
                setItem({ id: i.id, name: itemTitle(i), mrpPaise: effectiveMrp(i) });
                setPicking(false);
              }}
            />
          )}
          {tried && item === null && (
            <p role="alert" className="tz-field-error">
              {intl.formatMessage({ id: 'voice.edit.itemRequired' })}
            </p>
          )}
        </div>
        <Segmented
          label={intl.formatMessage({ id: 'voice.edit.kind' })}
          value={kind}
          onChange={setKind}
          options={KINDS.map((k) => ({ value: k, label: intl.formatMessage({ id: `voice.kind.${k}` }) }))}
        />
        {!canConfirmKind(store.role, kind) && <p className={styles.note}>{intl.formatMessage({ id: 'voice.line.managerNote' })}</p>}
        {kind === 'set_price' ? (
          <TextField
            label={intl.formatMessage({ id: 'voice.edit.price' })}
            prefix="₹"
            inputMode="decimal"
            value={price}
            onChange={(e) => setPrice(e.target.value)}
            help={line.currentPricePaise !== null ? intl.formatMessage({ id: 'voice.edit.priceNow' }, { price: formatMoney(line.currentPricePaise) }) : undefined}
            error={numberError}
          />
        ) : (
          <TextField
            label={intl.formatMessage({ id: kind === 'count' ? 'voice.edit.counted' : 'voice.edit.qty' })}
            inputMode="numeric"
            value={qty}
            onChange={(e) => setQty(e.target.value)}
            help={kind === 'count' ? intl.formatMessage({ id: 'voice.edit.countHelp' }) : undefined}
            error={numberError}
          />
        )}
      </form>
    </Sheet>
  );
}

/** Search the shop's items (listed or not: stock and prices change on both). */
function ItemSearch({ store, initial, onPick }: { store: CurrentStore; initial: string; onPick: (i: StoreItem) => void }) {
  const intl = useIntl();
  const [q, setQ] = useState(initial);
  const list = useItems(store, q.trim() ? { q: q.trim() } : {});
  const found = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  return (
    <div className={styles.search}>
      <SearchField
        label={intl.formatMessage({ id: 'voice.edit.search' })}
        placeholder={intl.formatMessage({ id: 'voice.edit.searchHint' })}
        value={q}
        onChange={setQ}
      />
      {list.isPending ? (
        <Skeleton height="4rem" />
      ) : list.isError ? (
        <p className="tz-field-error">{intl.formatMessage({ id: 'voice.error.generic' })}</p>
      ) : found.length === 0 ? (
        <p className={styles.note}>{intl.formatMessage({ id: 'voice.edit.noItems' })}</p>
      ) : (
        <ul className={styles.results} aria-label={intl.formatMessage({ id: 'voice.edit.results' })}>
          {found.slice(0, 20).map((i) => (
            <li key={i.id} className={styles.pickedRow}>
              <span className={styles.grow}>{itemTitle(i)}</span>
              <Button size="sm" onClick={() => onPick(i)} aria-label={intl.formatMessage({ id: 'voice.edit.pickOne' }, { name: itemTitle(i) })}>
                {intl.formatMessage({ id: 'voice.edit.pickShort' })}
              </Button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
