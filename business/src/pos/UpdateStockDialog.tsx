// D-270: the counter never sells more than an item's available units. When a bill wants more, this
// dialog asks for a count of the shelf instead. Saving writes a normal stock count (any role may
// count) through the outbox, so it works offline; the bill then takes what the count allows.
import { Banner, Button, Dialog, TextField } from '@todayz/ui';
import { useRef, useState } from 'react';
import { useIntl } from 'react-intl';
import { afterCount, blockedUnits, countedOnHand, itemLabel, onHandUnits, sellableUnits } from './catalogue';
import { usePos } from './context';
import type { CatalogueItem } from './types';
import styles from './pos.module.css';

/** Why the count is asked: a line wants more than is available, or an online sale found the shop short. */
export type StockAskReason = 'add' | 'charge' | 'sold';

export interface UpdateStockDialogProps {
  item: CatalogueItem | null;
  /** Units the bill wants of the item; 0 when nothing waits on the count. */
  wanted: number;
  reason: StockAskReason;
  onCancel: () => void;
  /** The item as the count left it. */
  onSaved: (item: CatalogueItem) => void;
}

const MAX_COUNT = 1_000_000;

export function UpdateStockDialog({ item, wanted, reason, onCancel, onSaved }: UpdateStockDialogProps) {
  const intl = useIntl();
  const { engine, online } = usePos();
  const [counted, setCounted] = useState('');
  const [includesBlocked, setIncludesBlocked] = useState(false);
  const [error, setError] = useState<'count' | 'save' | null>(null);
  const [saving, setSaving] = useState(false);
  const countRef = useRef<HTMLInputElement>(null);
  // Each item that opens the dialog starts it empty (state adjusted while rendering, not in an effect).
  const [shownId, setShownId] = useState<string | null>(item?.id ?? null);
  if ((item?.id ?? null) !== shownId) {
    setShownId(item?.id ?? null);
    setCounted('');
    setIncludesBlocked(false);
    setError(null);
  }

  const blocked = item ? blockedUnits(item) : 0;
  const n = /^\d+$/.test(counted.trim()) ? Number(counted.trim()) : null;
  const onHand = n === null ? null : countedOnHand(n, blocked, blocked > 0 && includesBlocked);
  const nextAvailable = onHand === null ? null : Math.max(0, onHand - blocked);

  const save = async () => {
    if (!item || saving) return;
    if (onHand === null || onHand > MAX_COUNT) return setError('count');
    setSaving(true);
    setError(null);
    try {
      await engine.recordMovement({ storeItemId: item.id, kind: 'count_correction', quantity: onHand });
      if (online) void engine.flush();
      onSaved(afterCount(item, onHand));
    } catch {
      setError('save');
    } finally {
      setSaving(false);
    }
  };

  const fmt = (id: string, values?: Record<string, string | number>) => intl.formatMessage({ id }, values);

  return (
    <Dialog
      open={item !== null}
      onClose={onCancel}
      title={fmt('pos.stock.title')}
      description={item ? fmt(`pos.stock.reason.${reason}`, { name: itemLabel(item), available: sellableUnits(item), wanted }) : undefined}
      closeLabel={fmt('pos.close')}
      initialFocusRef={countRef}
      footer={
        <>
          <Button variant="ghost" onClick={onCancel}>
            {fmt('pos.stock.cancel')}
          </Button>
          <Button variant="primary" loading={saving} onClick={() => void save()}>
            {fmt('pos.stock.save')}
          </Button>
        </>
      }
    >
      {item && (
        <form
          className={styles.sheetBody}
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <dl className={styles.stockFacts}>
            <div>
              <dt>{fmt('pos.stock.onHand')}</dt>
              <dd className="tz-num">{onHandUnits(item)}</dd>
            </div>
            <div>
              <dt>{fmt('pos.stock.blocked')}</dt>
              <dd className="tz-num">{blocked}</dd>
            </div>
            <div>
              <dt>{fmt('pos.stock.available')}</dt>
              <dd className="tz-num">{sellableUnits(item)}</dd>
            </div>
          </dl>
          {item.countNeeded && <Banner tone="warning">{fmt('pos.stock.countNeeded')}</Banner>}
          <TextField
            label={fmt('pos.stock.counted')}
            help={fmt('pos.stock.countedHelp')}
            inputMode="numeric"
            autoComplete="off"
            ref={countRef}
            value={counted}
            onChange={(e) => setCounted(e.target.value)}
            error={error === 'count' ? fmt('pos.stock.countedInvalid') : undefined}
          />
          {blocked > 0 && (
            <label className={styles.check}>
              <input type="checkbox" checked={includesBlocked} onChange={(e) => setIncludesBlocked(e.target.checked)} />
              <span>{fmt('pos.stock.includesBlocked', { count: blocked })}</span>
            </label>
          )}
          {nextAvailable !== null && (
            <p className={styles.stockAfter} aria-live="polite">
              {fmt('pos.stock.after', { onHand: onHand ?? 0, available: nextAvailable })}
            </p>
          )}
          {error === 'save' && <Banner tone="warning">{fmt('pos.stock.saveFailed')}</Banner>}
          {/* Enter in the field saves; the footer buttons sit outside the form. */}
          <button type="submit" hidden />
        </form>
      )}
    </Dialog>
  );
}
