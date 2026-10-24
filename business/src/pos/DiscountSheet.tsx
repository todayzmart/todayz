// Manual discounts (manager and above; hidden for cashiers, D-06, D-65): one amount per line and one
// for the whole bill, in rupees. The bill never drops below ₹1, so the cart caps the bill discount.
import { formatMoney } from '@todayz/i18n';
import { Button, Sheet, TextField } from '@todayz/ui';
import { useEffect, useState } from 'react';
import { useIntl } from 'react-intl';
import type { CartAction, CartState } from './cart';
import { parseRupees } from './money';
import styles from './pos.module.css';

export interface DiscountSheetProps {
  open: boolean;
  onClose: () => void;
  cart: CartState;
  dispatch: (a: CartAction) => void;
}

const asRupees = (paise: number) => (paise > 0 ? String(paise / 100) : '');

export function DiscountSheet({ open, onClose, cart, dispatch }: DiscountSheetProps) {
  const intl = useIntl();
  const [bill, setBill] = useState('');
  const [lines, setLines] = useState<Record<string, string>>({});
  const [invalid, setInvalid] = useState<Set<string>>(new Set());

  useEffect(() => {
    if (!open) return;
    setBill(asRupees(cart.billDiscountPaise));
    setLines(Object.fromEntries(cart.lines.map((l) => [l.id, asRupees(l.discountPaise)])));
    setInvalid(new Set());
    // Opening snapshots the cart; edits while it is open are the user's.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const apply = () => {
    const bad = new Set<string>();
    const parse = (key: string, text: string): number => {
      if (text.trim() === '') return 0;
      const p = parseRupees(text);
      if (p === null) bad.add(key);
      return p ?? 0;
    };
    const next = cart.lines.map((l) => ({ id: l.id, paise: parse(l.id, lines[l.id] ?? '') }));
    const billPaise = parse('bill', bill);
    setInvalid(bad);
    if (bad.size > 0) return;
    for (const n of next) dispatch({ type: 'lineDiscount', lineId: n.id, discountPaise: n.paise });
    dispatch({ type: 'billDiscount', discountPaise: billPaise });
    onClose();
  };

  return (
    <Sheet open={open} onClose={onClose} title={intl.formatMessage({ id: 'pos.discount.title' })} description={intl.formatMessage({ id: 'pos.discount.help' })} closeLabel={intl.formatMessage({ id: 'pos.close' })}>
      <form
        className={styles.sheetBody}
        onSubmit={(e) => {
          e.preventDefault();
          apply();
        }}
      >
        {cart.lines.map((l) => (
          <TextField
            key={l.id}
            label={intl.formatMessage({ id: 'pos.discount.line' }, { name: l.name, price: formatMoney(l.unitPricePaise * l.quantity, 'product') })}
            inputMode="decimal"
            prefix="₹"
            value={lines[l.id] ?? ''}
            onChange={(e) => setLines((m) => ({ ...m, [l.id]: e.target.value }))}
            error={invalid.has(l.id) ? intl.formatMessage({ id: 'pos.discount.invalid' }) : undefined}
          />
        ))}
        <TextField
          label={intl.formatMessage({ id: 'pos.discount.bill' })}
          inputMode="decimal"
          prefix="₹"
          value={bill}
          onChange={(e) => setBill(e.target.value)}
          error={invalid.has('bill') ? intl.formatMessage({ id: 'pos.discount.invalid' }) : undefined}
        />
        <Button type="submit" variant="primary">
          {intl.formatMessage({ id: 'pos.discount.apply' })}
        </Button>
      </form>
    </Sheet>
  );
}
