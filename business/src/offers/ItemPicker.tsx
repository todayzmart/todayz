import type { StoreItem } from '@todayz/api-client/gen/model/storeItem';
import { Button, IconButton, SearchField, Skeleton } from '@todayz/ui';
import { X } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { itemTitle } from '../stock/format';
import { useItems } from '../stock/queries';
import type { PickedItem } from './form';
import { toPicked } from './picked';
import styles from './offers.module.css';

export interface ItemPickerProps {
  store: CurrentStore;
  picked: readonly PickedItem[];
  /** 1 = replace the choice; more = add up to this many. */
  max: number;
  onChange: (items: PickedItem[]) => void;
}

/** Search the shop's listed items and tick the ones the offer is on. */
export function ItemPicker({ store, picked, max, onChange }: ItemPickerProps) {
  const intl = useIntl();
  const [q, setQ] = useState('');
  const list = useItems(store, q.trim() ? { q: q.trim(), listed: true } : { listed: true });
  const found = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  const has = (id: string) => picked.some((p) => p.id === id);
  const add = (i: StoreItem) => onChange(max === 1 ? [toPicked(i)] : [...picked, toPicked(i)].slice(0, max));

  return (
    <div className={styles.picker}>
      {picked.length > 0 && (
        <ul className={styles.picked} aria-label={intl.formatMessage({ id: 'offers.picker.picked' })}>
          {picked.map((p) => (
            <li key={p.id} className={styles.pickedRow}>
              <span className={styles.grow}>{p.title}</span>
              <IconButton
                size="sm"
                variant="ghost"
                icon={X}
                label={intl.formatMessage({ id: 'offers.picker.remove' }, { name: p.title })}
                onClick={() => onChange(picked.filter((x) => x.id !== p.id))}
              />
            </li>
          ))}
        </ul>
      )}
      <SearchField
        label={intl.formatMessage({ id: 'offers.picker.search' })}
        placeholder={intl.formatMessage({ id: 'offers.picker.searchHint' })}
        value={q}
        onChange={setQ}
      />
      {list.isPending ? (
        <Skeleton height="4rem" />
      ) : list.isError ? (
        <p className="tz-field-error">{intl.formatMessage({ id: 'offers.error.generic' })}</p>
      ) : found.length === 0 ? (
        <p className={styles.note}>{intl.formatMessage({ id: 'offers.picker.none' })}</p>
      ) : (
        <ul className={styles.results} aria-label={intl.formatMessage({ id: 'offers.picker.results' })}>
          {found.slice(0, 20).map((i) => (
            <li key={i.id} className={styles.resultRow}>
              <span className={styles.grow}>{itemTitle(i)}</span>
              <Button size="sm" disabled={has(i.id) && max !== 1} onClick={() => add(i)}>
                {intl.formatMessage({ id: has(i.id) ? 'offers.picker.added' : 'offers.picker.add' })}
              </Button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
