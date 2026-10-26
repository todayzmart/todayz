import type { StockBatch } from '@todayz/api-client/gen/model/stockBatch';
import type { StoreItem } from '@todayz/api-client/gen/model/storeItem';
import { formatMoney } from '@todayz/i18n';
import { Button, Sheet, Skeleton, StockTag, Tabs } from '@todayz/ui';
import { Eye, EyeOff } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { useSession } from '@todayz/auth';
import { useQueryClient } from '@tanstack/react-query';
import { useIdempotentAction } from './action';
import { BatchForm, PriceForm, ThresholdForm } from './ItemForms';
import { effectiveMrp, itemTitle, stockLevel } from './format';
import { dateText, freshnessProps, levelLabel, sourceLabel, whenText } from './labels';
import { MovementForm, type MovementFormKind } from './MovementForm';
import { canStock, type StockAction } from './permissions';
import { patchItem, useBatches, useHistory, useItem } from './queries';
import { StockError } from './StockError';
import styles from './stock.module.css';
import { Thumb } from './Thumb';
import type { CurrentStore } from '../orders/store';

export type SheetMode = 'view' | MovementFormKind | 'price' | 'threshold' | { batch: string };

export interface ItemSheetProps {
  store: CurrentStore;
  /** The row the sheet opened from; the sheet reads the live copy from the cache. */
  item: StoreItem;
  /** What to open first (a deep link). A mode the role may not use opens the view with "Ask a manager". */
  initialMode?: SheetMode | undefined;
  initialQty?: number | undefined;
  initialBatchIds?: readonly string[] | undefined;
  /** Reference price (paise) from a price insight. */
  reference?: number | undefined;
  onClose: () => void;
}

const MODE_ACTION: Record<MovementFormKind | 'price' | 'threshold', StockAction> = {
  restock: 'restock',
  damage: 'damage',
  expired: 'damage',
  count_correction: 'count',
  price: 'price',
  threshold: 'manage',
};

function allowed(role: CurrentStore['role'], mode: SheetMode): boolean {
  if (mode === 'view') return true;
  if (typeof mode === 'object') return canStock(role, 'batches');
  return canStock(role, MODE_ACTION[mode]);
}

/** One item: stock facts, the writes its role may do, batches and history. */
export function ItemSheet({ store, item: row, initialMode = 'view', initialQty, initialBatchIds, reference, onClose }: ItemSheetProps) {
  const intl = useIntl();
  const live = useItem(store, row.id);
  const item = live.data ?? row;
  const refused = !allowed(store.role, initialMode);
  const [mode, setMode] = useState<SheetMode>(refused ? 'view' : initialMode);
  const [tab, setTab] = useState<'batches' | 'history'>('batches');
  const batches = useBatches(store, item.id);
  const back = () => setMode('view');
  const mrp = effectiveMrp(item);

  const button = (m: MovementFormKind | 'price' | 'threshold', label: string, show: boolean) =>
    show && (
      <Button key={m} size="sm" onClick={() => setMode(m)}>
        {label}
      </Button>
    );

  const editingBatch = typeof mode === 'object' ? batches.data?.items.find((b) => b.id === mode.batch) : undefined;

  return (
    <Sheet open onClose={onClose} title={itemTitle(item)} closeLabel={intl.formatMessage({ id: 'stock.item.close' })}>
      <div className={styles.sheetBody}>
        <div className={styles.photoField}>
          <Thumb item={item} large />
          <StockTag
            level={stockLevel(item)}
            label={levelLabel(intl, stockLevel(item), item.available)}
            {...freshnessProps(intl, item)}
          />
        </div>
        {item.photo?.status === 'pending' && <p className={styles.sub}>{intl.formatMessage({ id: 'stock.photo.pending' })}</p>}
        {item.photo?.status === 'rejected' && (
          <p className={styles.sub}>
            {item.photo.rejectReason
              ? intl.formatMessage({ id: 'stock.photo.rejectedWhy' }, { reason: item.photo.rejectReason })
              : intl.formatMessage({ id: 'stock.photo.rejected' })}
          </p>
        )}
        {refused && <p className={styles.note}>{intl.formatMessage({ id: 'stock.link.askManager' })}</p>}
        <dl className={styles.facts}>
          <Fact label={intl.formatMessage({ id: 'stock.item.onHand' })} value={String(item.onHand)} />
          <Fact label={intl.formatMessage({ id: 'stock.item.reserved' })} value={String(item.reserved)} />
          <Fact label={intl.formatMessage({ id: 'stock.item.available' })} value={String(item.available)} />
          <Fact label={intl.formatMessage({ id: 'stock.item.price' })} value={formatMoney(item.pricePaise, 'product')} />
          {mrp !== null && <Fact label={intl.formatMessage({ id: 'stock.item.mrp' })} value={formatMoney(mrp, 'product')} />}
          <Fact
            label={intl.formatMessage({ id: 'stock.item.lowAt' })}
            value={item.lowStockAt === null ? intl.formatMessage({ id: 'stock.item.notSet' }) : String(item.lowStockAt)}
          />
        </dl>

        {mode === 'view' && (
          <div className={styles.actions}>
            {button('restock', intl.formatMessage({ id: 'stock.act.restock' }), canStock(store.role, 'restock'))}
            {button('count_correction', intl.formatMessage({ id: 'stock.act.count' }), canStock(store.role, 'count'))}
            {button('damage', intl.formatMessage({ id: 'stock.act.damage' }), canStock(store.role, 'damage'))}
            {button('expired', intl.formatMessage({ id: 'stock.act.expired' }), canStock(store.role, 'damage'))}
            {button('price', intl.formatMessage({ id: 'stock.act.price' }), canStock(store.role, 'price'))}
            {button('threshold', intl.formatMessage({ id: 'stock.act.threshold' }), canStock(store.role, 'manage'))}
            {canStock(store.role, 'manage') && <ListToggle store={store} item={item} />}
          </div>
        )}

        {(mode === 'restock' || mode === 'damage' || mode === 'expired' || mode === 'count_correction') && (
          <MovementForm
            store={store}
            item={item}
            kind={mode}
            batches={batches.data?.items}
            initialQuantity={mode === 'restock' ? initialQty : undefined}
            initialBatchIds={mode === 'expired' ? initialBatchIds : undefined}
            onDone={back}
            onCancel={back}
          />
        )}
        {mode === 'price' && <PriceForm store={store} item={item} reference={reference} onDone={back} onCancel={back} />}
        {mode === 'threshold' && <ThresholdForm store={store} item={item} onDone={back} onCancel={back} />}
        {typeof mode === 'object' && editingBatch && <BatchForm store={store} batch={editingBatch} onDone={back} onCancel={back} />}

        <Tabs
          label={intl.formatMessage({ id: 'stock.item.tabs' })}
          value={tab}
          onChange={setTab}
          tabs={[
            { value: 'batches', label: intl.formatMessage({ id: 'stock.tab.batches' }) },
            { value: 'history', label: intl.formatMessage({ id: 'stock.tab.history' }) },
          ]}
        />
        {tab === 'batches' ? (
          <BatchList
            query={batches}
            canEdit={canStock(store.role, 'batches')}
            onEdit={(b) => setMode({ batch: b.id })}
          />
        ) : (
          <History store={store} itemId={item.id} />
        )}
      </div>
    </Sheet>
  );
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className={styles.fact}>
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}

/** List or hide the item: hidden items stay in the stock list but customers do not see them. */
function ListToggle({ store, item }: { store: CurrentStore; item: StoreItem }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const session = useSession();
  const action = useIdempotentAction();
  return (
    <>
      <Button
        size="sm"
        icon={item.isListed ? EyeOff : Eye}
        loading={action.pending}
        onClick={() => void action.run(`listed:${String(!item.isListed)}`, () => patchItem(qc, session, store, item.id, { isListed: !item.isListed }))}
      >
        {intl.formatMessage({ id: item.isListed ? 'stock.act.hide' : 'stock.act.list' })}
      </Button>
      <StockError error={action.error} />
    </>
  );
}

function BatchList({
  query,
  canEdit,
  onEdit,
}: {
  query: ReturnType<typeof useBatches>;
  canEdit: boolean;
  onEdit: (b: StockBatch) => void;
}) {
  const intl = useIntl();
  if (query.isError) return <p className="tz-field-error">{intl.formatMessage({ id: 'stock.error.generic' })}</p>;
  if (!query.data) return <Skeleton height="3rem" />;
  const live = query.data.items.filter((b) => b.quantity > 0);
  if (live.length === 0) return <p className={styles.sub}>{intl.formatMessage({ id: 'stock.batch.none' })}</p>;
  return (
    <ul className={styles.rows} aria-label={intl.formatMessage({ id: 'stock.tab.batches' })}>
      {live.map((b) => (
        <li key={b.id} className={styles.row}>
          <span className={styles.cell}>
            <span>{intl.formatMessage({ id: 'stock.batch.left' }, { quantity: b.quantity })}</span>
            <span className={styles.sub}>
              {b.expiresOn
                ? intl.formatMessage({ id: b.expired ? 'stock.batch.expiredOn' : 'stock.batch.expires' }, { date: dateText(intl, b.expiresOn) })
                : intl.formatMessage({ id: 'stock.batch.noExpiry' })}
              {b.costPaise !== null && ` · ${intl.formatMessage({ id: 'stock.batch.cost' }, { cost: formatMoney(b.costPaise, 'product') })}`}
            </span>
          </span>
          {canEdit && (
            <Button size="sm" variant="ghost" onClick={() => onEdit(b)}>
              {intl.formatMessage({ id: 'stock.batch.edit' })}
            </Button>
          )}
        </li>
      ))}
    </ul>
  );
}

function History({ store, itemId }: { store: CurrentStore; itemId: string }) {
  const intl = useIntl();
  const q = useHistory(store, itemId);
  if (q.isError) return <p className="tz-field-error">{intl.formatMessage({ id: 'stock.error.generic' })}</p>;
  if (!q.data) return <Skeleton height="3rem" />;
  const rows = q.data.pages.flatMap((p) => p.items);
  if (rows.length === 0) return <p className={styles.sub}>{intl.formatMessage({ id: 'stock.history.none' })}</p>;
  return (
    <>
      <ul className={styles.rows} aria-label={intl.formatMessage({ id: 'stock.tab.history' })}>
        {rows.map((m) => (
          <li key={m.id} className={styles.row}>
            <span className={styles.cell}>
              <span>{sourceLabel(intl, m.source)}</span>
              <span className={styles.sub}>
                {whenText(intl, m.createdAt)}
                {m.note ? ` · ${m.note}` : ''}
              </span>
            </span>
            <span className={styles.delta}>{m.delta > 0 ? `+${m.delta}` : String(m.delta)}</span>
          </li>
        ))}
      </ul>
      {q.hasNextPage && (
        <div className={styles.more}>
          <Button size="sm" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
            {intl.formatMessage({ id: 'stock.more' })}
          </Button>
        </div>
      )}
    </>
  );
}
