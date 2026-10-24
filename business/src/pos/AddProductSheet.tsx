// A scanned code the catalogue does not know, or a one-off item: add it to the bill as a custom
// line (name and price), and point to Stock for adding the real product.
import { Link } from '@tanstack/react-router';
import { Banner, Button, Sheet, TextField } from '@todayz/ui';
import { useEffect, useState } from 'react';
import { useIntl } from 'react-intl';
import { parseRupees } from './money';
import styles from './pos.module.css';

export interface AddProductSheetProps {
  open: boolean;
  onClose: () => void;
  /** The unknown code that opened the sheet; absent for a plain custom item. */
  barcode: string | null;
  onAdd: (name: string, unitPricePaise: number) => void;
}

export function AddProductSheet({ open, onClose, barcode, onAdd }: AddProductSheetProps) {
  const intl = useIntl();
  const [name, setName] = useState('');
  const [price, setPrice] = useState('');
  const [error, setError] = useState<'name' | 'price' | null>(null);
  useEffect(() => {
    if (open) {
      setName('');
      setPrice('');
      setError(null);
    }
  }, [open]);

  const submit = () => {
    const paise = parseRupees(price);
    if (!name.trim()) return setError('name');
    if (paise === null || paise <= 0) return setError('price');
    onAdd(name.trim(), paise);
    onClose();
  };

  return (
    <Sheet open={open} onClose={onClose} title={intl.formatMessage({ id: barcode ? 'pos.add.unknownTitle' : 'pos.add.customTitle' })} closeLabel={intl.formatMessage({ id: 'pos.close' })}>
      <form
        className={styles.sheetBody}
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        {barcode && <Banner>{intl.formatMessage({ id: 'pos.add.unknownBody' }, { code: barcode })}</Banner>}
        <TextField
          label={intl.formatMessage({ id: 'pos.add.name' })}
          autoComplete="off"
          value={name}
          onChange={(e) => setName(e.target.value)}
          error={error === 'name' ? intl.formatMessage({ id: 'pos.add.nameInvalid' }) : undefined}
        />
        <TextField
          label={intl.formatMessage({ id: 'pos.add.price' })}
          inputMode="decimal"
          prefix="₹"
          value={price}
          onChange={(e) => setPrice(e.target.value)}
          error={error === 'price' ? intl.formatMessage({ id: 'pos.add.priceInvalid' }) : undefined}
        />
        <Button type="submit" variant="primary">
          {intl.formatMessage({ id: 'pos.add.submit' })}
        </Button>
        {barcode && (
          // TODO(lead): the add-product screen is W2-12 (Stock). Until its route exists this opens the Stock section.
          <Link to="/$section" params={{ section: 'stock' }} className="tz-btn tz-btn-ghost">
            {intl.formatMessage({ id: 'pos.add.inStock' })}
          </Link>
        )}
      </form>
    </Sheet>
  );
}
