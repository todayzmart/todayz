import type { StoreItem } from '@todayz/api-client/gen/model/storeItem';
import { newUlid, useSession } from '@todayz/auth';
import { formatMoney } from '@todayz/i18n';
import { Banner, Button, Chip, DataTable, EmptyState, SearchField, Skeleton, StockTag, useToast, type ColumnDef } from '@todayz/ui';
import { Link, useNavigate } from '@tanstack/react-router';
import { useQueryClient } from '@tanstack/react-query';
import { ClipboardCheck, Inbox, Plus, Store as StoreIcon, Upload } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { useCurrentStore, type CurrentStore } from '../orders/store';
import { AddItem } from './AddItem';
import type { StockSearch } from './deeplink';
import { itemTitle, parseQuantity, parseRupees, rupeesText, stockLevel } from './format';
import { ImportSheet } from './ImportSheet';
import { InlineEdit } from './InlineEdit';
import { ItemSheet, type SheetMode } from './ItemSheet';
import { freshnessProps, levelLabel } from './labels';
import { VoiceLink } from '../voice/VoiceLink';
import { canStock } from './permissions';
import { patchItem, sendMovement, useCategories, useItems, useItemsById, type ItemFilters } from './queries';
import styles from './stock.module.css';

type Filter = 'all' | 'low' | 'out' | 'stale' | 'hidden';

/** B-07 Stock: the shop's items, inline price and count, and the deep-link entry points (D-99). */
export function StockPage({ search }: { search: StockSearch }) {
  const intl = useIntl();
  const current = useCurrentStore();
  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'stock.title' })}</h1>
      {current.status === 'loading' && (
        <div role="status" aria-label={intl.formatMessage({ id: 'stock.loading' })}>
          <Skeleton height="6rem" />
        </div>
      )}
      {current.status === 'error' && (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'stock.error.title' })}
          description={intl.formatMessage({ id: 'stock.error.body' })}
          action={<Button onClick={current.retry}>{intl.formatMessage({ id: 'stock.error.retry' })}</Button>}
        />
      )}
      {current.status === 'none' && (
        <EmptyState
          icon={StoreIcon}
          title={intl.formatMessage({ id: 'stock.noStore.title' })}
          description={intl.formatMessage({ id: 'stock.noStore.body' })}
          action={
            <Link to="/choose-shop" search={{ next: '/stock' }} className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'stock.noStore.action' })}
            </Link>
          }
        />
      )}
      {current.status === 'ready' && <StockDesk store={current.store} search={search} />}
    </div>
  );
}

interface Open {
  item: StoreItem;
  mode: SheetMode;
  qty?: number | undefined;
  batchIds?: readonly string[] | undefined;
  ref?: number | undefined;
}

function StockDesk({ store, search }: { store: CurrentStore; search: StockSearch }) {
  const intl = useIntl();
  const navigate = useNavigate();
  const [q, setQ] = useState('');
  const [filter, setFilter] = useState<Filter>('all');
  const [categoryId, setCategoryId] = useState('');
  const [counting, setCounting] = useState(search.count !== undefined);
  const [open, setOpen] = useState<Open | null>(null);
  const [adding, setAdding] = useState(search.add !== undefined || search.add_q !== undefined);
  const [importing, setImporting] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const categories = useCategories();
  const addLink = search.add !== undefined || search.add_q !== undefined;

  const filters: ItemFilters = {
    ...(q.trim().length >= 2 ? { q: q.trim() } : {}),
    ...(categoryId ? { categoryId } : {}),
    ...(filter === 'low' ? { lowStock: true } : {}),
    ...(filter === 'hidden' ? { listed: false } : {}),
  };
  const list = useItems(store, filters);
  const countIds = counting && search.count ? search.count : [];
  const byId = useItemsById(store, countIds);
  const linkIds = useMemo(
    () => [search.restock, search.expire, search.price, search.batch ? search.item : undefined].filter((x): x is string => x !== undefined),
    [search.restock, search.expire, search.price, search.batch, search.item],
  );
  const linked = useItemsById(store, linkIds);

  const all = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  const rows = useMemo(() => {
    const base = countIds.length > 0 ? byId.flatMap((r) => (r.data ? [r.data] : [])) : all;
    return base.filter((i) => (filter === 'out' ? i.available <= 0 : filter === 'stale' ? i.stale : true));
  }, [all, byId, countIds.length, filter]);

  // A deep link opens its sheet once the item is known; an id that is not in this shop opens nothing.
  const fromLink = useMemo((): Open | null => {
    const find = (id: string | undefined) => linked.find((r) => r.data?.id === id)?.data;
    if (search.restock) {
      const item = find(search.restock);
      return item ? { item, mode: 'restock', qty: search.qty } : null;
    }
    if (search.expire) {
      const item = find(search.expire);
      return item ? { item, mode: 'expired', batchIds: search.batches } : null;
    }
    if (search.price) {
      const item = find(search.price);
      return item ? { item, mode: 'price', ref: search.ref } : null;
    }
    if (search.batch && search.item) {
      const item = find(search.item);
      return item ? { item, mode: { batch: search.batch } } : null;
    }
    return null;
  }, [linked, search]);
  const sheet = open ?? (dismissed ? null : fromLink);

  const closeSheet = () => {
    setOpen(null);
    setDismissed(true);
    if (linkIds.length > 0) void navigate({ to: '/stock', search: {}, replace: true });
  };

  return (
    <>
      <div className={styles.head}>
        <p className={styles.sub}>{store.name}</p>
        <div className={styles.headActions}>
          <VoiceLink store={store} />
          <Button icon={ClipboardCheck} variant={counting ? 'primary' : 'default'} onClick={() => setCounting((c) => !c)} aria-pressed={counting}>
            {intl.formatMessage({ id: counting ? 'stock.action.countDone' : 'stock.action.count' })}
          </Button>
          {canStock(store.role, 'import') && (
            <Button icon={Upload} onClick={() => setImporting(true)}>
              {intl.formatMessage({ id: 'stock.action.import' })}
            </Button>
          )}
          {canStock(store.role, 'addItem') && (
            <Button icon={Plus} variant="primary" onClick={() => setAdding(true)}>
              {intl.formatMessage({ id: 'stock.action.add' })}
            </Button>
          )}
        </div>
      </div>

      {addLink && !canStock(store.role, 'addItem') && <Banner tone="warning">{intl.formatMessage({ id: 'stock.link.askManager' })}</Banner>}
      {counting && (
        <div className={styles.countBar}>
          <span>{intl.formatMessage({ id: 'stock.count.hint' })}</span>
        </div>
      )}

      <div className={styles.toolbar}>
        <SearchField
          label={intl.formatMessage({ id: 'stock.search.label' })}
          placeholder={intl.formatMessage({ id: 'stock.search.placeholder' })}
          value={q}
          onChange={setQ}
          clearLabel={intl.formatMessage({ id: 'stock.search.clear' })}
        />
        <div className={styles.chips} role="group" aria-label={intl.formatMessage({ id: 'stock.filter.label' })}>
          {(['all', 'low', 'out', 'stale', 'hidden'] as const).map((f) => (
            <Chip key={f} pressed={filter === f} onPressedChange={() => setFilter(f)}>
              {intl.formatMessage({ id: `stock.filter.${f}` })}
            </Chip>
          ))}
        </div>
        {categories.data && categories.data.items.length > 0 && (
          <select
            className="tz-input"
            aria-label={intl.formatMessage({ id: 'stock.category.label' })}
            value={categoryId}
            onChange={(e) => setCategoryId(e.target.value)}
          >
            <option value="">{intl.formatMessage({ id: 'stock.category.all' })}</option>
            {categories.data.items.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </select>
        )}
      </div>

      {list.isError ? (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'stock.error.title' })}
          description={intl.formatMessage({ id: 'stock.error.body' })}
          action={<Button onClick={() => void list.refetch()}>{intl.formatMessage({ id: 'stock.error.retry' })}</Button>}
        />
      ) : (
        <ItemTable
          store={store}
          rows={rows}
          loading={list.isPending}
          counting={counting}
          onOpen={(item) => setOpen({ item, mode: 'view' })}
          empty={all.length === 0 && filter === 'all' && !q}
        />
      )}
      {list.hasNextPage && countIds.length === 0 && (
        <div className={styles.more}>
          <Button loading={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
            {intl.formatMessage({ id: 'stock.more' })}
          </Button>
        </div>
      )}

      {sheet && (
        <ItemSheet
          key={sheet.item.id}
          store={store}
          item={sheet.item}
          initialMode={sheet.mode}
          initialQty={sheet.qty}
          initialBatchIds={sheet.batchIds}
          reference={sheet.ref}
          onClose={closeSheet}
        />
      )}
      {adding && canStock(store.role, 'addItem') && (
        <AddItem
          store={store}
          initialQuery={search.add_q}
          hadProductId={search.add !== undefined}
          onClose={() => {
            setAdding(false);
            if (addLink) void navigate({ to: '/stock', search: {}, replace: true });
          }}
        />
      )}
      {importing && <ImportSheet store={store} onClose={() => setImporting(false)} />}
    </>
  );
}

function ItemTable({
  store,
  rows,
  loading,
  counting,
  empty,
  onOpen,
}: {
  store: CurrentStore;
  rows: readonly StoreItem[];
  loading: boolean;
  counting: boolean;
  empty: boolean;
  onOpen: (i: StoreItem) => void;
}) {
  const intl = useIntl();
  const qc = useQueryClient();
  const session = useSession();
  const toast = useToast();

  const columns = useMemo<ColumnDef<StoreItem, unknown>[]>(
    () => [
      {
        id: 'item',
        header: intl.formatMessage({ id: 'stock.col.item' }),
        accessorFn: (i) => itemTitle(i),
        cell: ({ row }) => {
          const i = row.original;
          return (
            <span className={styles.cell}>
              <button type="button" className={styles.nameButton} onClick={() => onOpen(i)}>
                <span className={styles.name}>{itemTitle(i)}</span>
              </button>
              {!i.isListed && <span className={styles.sub}>{intl.formatMessage({ id: 'stock.item.hidden' })}</span>}
            </span>
          );
        },
      },
      {
        id: 'stock',
        header: intl.formatMessage({ id: 'stock.col.stock' }),
        accessorFn: (i) => i.available,
        cell: ({ row }) => {
          const i = row.original;
          return <StockTag level={stockLevel(i)} label={levelLabel(intl, stockLevel(i), i.available)} {...freshnessProps(intl, i)} />;
        },
      },
      {
        id: 'onHand',
        header: intl.formatMessage({ id: counting ? 'stock.col.counted' : 'stock.col.onHand' }),
        accessorFn: (i) => i.onHand,
        meta: { numeric: true },
        cell: ({ row }) => {
          const i = row.original;
          const name = itemTitle(i);
          return (
            <InlineEdit
              key={`${i.id}-${String(i.onHand)}`}
              label={intl.formatMessage({ id: 'stock.inline.count' }, { item: name })}
              initial={String(i.onHand)}
              parse={parseQuantity}
              editable={counting && canStock(store.role, 'count')}
              always={counting}
              display={String(i.onHand)}
              invalidText={intl.formatMessage({ id: 'stock.form.countInvalid' })}
              failedText={intl.formatMessage({ id: 'stock.error.generic' })}
              saveLabel={intl.formatMessage({ id: 'stock.inline.confirm' }, { item: name })}
              cancelLabel={intl.formatMessage({ id: 'stock.form.cancel' })}
              editLabel={intl.formatMessage({ id: 'stock.inline.edit' }, { item: name })}
              onSave={async (n) => {
                try {
                  await sendMovement(qc, store, i.id, { kind: 'count_correction', quantity: n }, newUlid());
                  toast.show({ message: intl.formatMessage({ id: 'stock.toast.count_correction' }, { item: name }) });
                  return true;
                } catch {
                  return false;
                }
              }}
            />
          );
        },
      },
      {
        id: 'price',
        header: intl.formatMessage({ id: 'stock.col.price' }),
        accessorFn: (i) => i.pricePaise,
        meta: { numeric: true },
        cell: ({ row }) => {
          const i = row.original;
          const name = itemTitle(i);
          return (
            <InlineEdit
              key={`${i.id}-${String(i.pricePaise)}`}
              label={intl.formatMessage({ id: 'stock.inline.price' }, { item: name })}
              initial={rupeesText(i.pricePaise)}
              prefix="₹"
              parse={parseRupees}
              editable={!counting && canStock(store.role, 'price')}
              display={formatMoney(i.pricePaise, 'product')}
              invalidText={intl.formatMessage({ id: 'stock.form.priceInvalid' })}
              failedText={intl.formatMessage({ id: 'stock.error.generic' })}
              saveLabel={intl.formatMessage({ id: 'stock.inline.save' }, { item: name })}
              cancelLabel={intl.formatMessage({ id: 'stock.form.cancel' })}
              editLabel={intl.formatMessage({ id: 'stock.inline.edit' }, { item: name })}
              onSave={async (p) => {
                try {
                  await patchItem(qc, session, store, i.id, { pricePaise: p });
                  toast.show({ message: intl.formatMessage({ id: 'stock.toast.price' }, { item: name }) });
                  return true;
                } catch {
                  return false;
                }
              }}
            />
          );
        },
      },
    ],
    [intl, counting, onOpen, qc, session, store, toast],
  );

  return (
    <DataTable
      label={intl.formatMessage({ id: 'stock.table.label' })}
      columns={columns}
      data={rows}
      getRowId={(i) => i.id}
      loading={loading}
      emptyLabel={intl.formatMessage({ id: empty ? 'stock.empty.none' : 'stock.empty.filtered' })}
    />
  );
}
