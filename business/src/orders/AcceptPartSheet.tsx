import type { AcceptLine } from '@todayz/api-client/gen/model/acceptLine';
import type { StoreOrder } from '@todayz/api-client/gen/model/storeOrder';
import { Button, QuantityStepper, Sheet } from '@todayz/ui';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { ActionError } from './ActionError';
import { canGive, lineName } from './labels';
import styles from './orders.module.css';

export interface AcceptPartSheetProps {
  order: StoreOrder;
  onClose: () => void;
  busy: boolean;
  error: unknown;
  onConfirm: (lines: AcceptLine[]) => void;
}

/**
 * Accept part (D-28, BK-08): a quantity for each line, prefilled with what the stock covers. The
 * customer then takes the changed order or cancels it; nothing is held until they confirm. Giving
 * nothing is a reject, and giving everything is a plain Accept, so both keep Send disabled.
 * Mount it only while open: each opening starts from the stock again.
 */
export function AcceptPartSheet({ order, onClose, busy, error, onConfirm }: AcceptPartSheetProps) {
  const intl = useIntl();
  const [qty, setQty] = useState<Record<string, number>>(() => Object.fromEntries(order.lines.map((l) => [l.id, canGive(l)])));
  const total = order.lines.reduce((n, l) => n + (qty[l.id] ?? 0), 0);
  const full = order.lines.every((l) => qty[l.id] === l.quantityRequested);
  const hint = total === 0 ? 'orders.part.none' : full ? 'orders.part.full' : null;
  return (
    <Sheet
      open
      onClose={onClose}
      title={intl.formatMessage({ id: 'orders.part.title' }, { number: order.number })}
      description={intl.formatMessage({ id: 'orders.part.body' })}
      closeLabel={intl.formatMessage({ id: 'orders.sheet.close' })}
      footer={
        <>
          <Button onClick={onClose}>{intl.formatMessage({ id: 'orders.sheet.back' })}</Button>
          <Button
            variant="primary"
            disabled={hint !== null}
            aria-describedby={hint ? 'part-hint' : undefined}
            loading={busy}
            onClick={() => onConfirm(order.lines.map((l) => ({ orderItemId: l.id, quantity: qty[l.id] ?? 0 })))}
          >
            {intl.formatMessage({ id: 'orders.part.confirm' })}
          </Button>
        </>
      }
    >
      <ul className={styles.partLines}>
        {order.lines.map((l) => (
          <li key={l.id} className={styles.partLine}>
            <span className={styles.partName}>
              <span>{lineName(l)}</span>
              <span className={styles.lineStock}>
                {intl.formatMessage({ id: 'orders.part.asked' }, { wanted: l.quantityRequested, available: Math.max(0, l.available) })}
              </span>
            </span>
            <QuantityStepper
              alwaysStepper
              size="sm"
              quantity={qty[l.id] ?? 0}
              max={canGive(l)}
              itemName={lineName(l)}
              onChange={(n) => setQty((q) => ({ ...q, [l.id]: n }))}
              labels={{
                add: intl.formatMessage({ id: 'orders.part.add' }),
                decrement: intl.formatMessage({ id: 'orders.part.less' }),
                increment: intl.formatMessage({ id: 'orders.part.more' }),
              }}
            />
          </li>
        ))}
      </ul>
      {hint && (
        <p id="part-hint" className={styles.shortHint}>
          {intl.formatMessage({ id: hint })}
        </p>
      )}
      <ActionError error={error} />
    </Sheet>
  );
}
