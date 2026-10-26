import { ApiError } from '@todayz/api-client';
import type { FieldBusiness } from '@todayz/api-client/gen/model/fieldBusiness';
import type { FieldItem } from '@todayz/api-client/gen/model/fieldItem';
import type { FieldProductMatch } from '@todayz/api-client/gen/model/fieldProductMatch';
import type { FieldUnit } from '@todayz/api-client/gen/model/fieldUnit';
import { Button, SearchField, StatusPill, TextField, useToast } from '@todayz/ui';
import { Plus } from 'lucide-react';
import { useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { money } from '../app/format';
import { useProblemText } from '../app/problems';
import { toPaise, UNITS } from './labels';
import { useAddItem, useCategories, useItems, useProductSearch, useUpdateItem } from './queries';
import css from './field.module.css';

const pack = (p: { packSize: number; unit: FieldUnit; variant: string | null }) => [p.variant, `${p.packSize} ${p.unit}`].filter(Boolean).join(' · ');

/** "4550" paise -> "45.50" for an input. */
const rupees = (p: number | null) => (p === null ? '' : (p / 100).toFixed(2).replace(/\.00$/, ''));

/**
 * Step 4: products one by one, from the master catalogue or as new (they wait in A-08 review). Open
 * until the edit window closes; the server answers 403 after it, and the list says so.
 */
export function ItemsStep({ business: b, onDone }: { business: FieldBusiness; onDone: () => void }) {
  const intl = useIntl();
  const [storeId, setStoreId] = useState(b.stores[0]?.id ?? '');
  if (b.stores.length === 0) return <p className={css.muted}>{intl.formatMessage({ id: 'field.items.noStore' })}</p>;
  return (
    <div className={css.form}>
      {b.stores.length > 1 && (
        <div className="tz-field">
          <label className="tz-field-label" htmlFor="field-items-store">
            {intl.formatMessage({ id: 'field.items.store' })}
          </label>
          <select id="field-items-store" className="tz-input" value={storeId} onChange={(e) => setStoreId(e.target.value)}>
            {b.stores.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </div>
      )}
      <StoreItems key={storeId} bid={b.id} sid={storeId} editable={b.itemsEditable} />
      {b.itemsEditable && (
        <Button variant="primary" block onClick={onDone}>
          {intl.formatMessage({ id: 'field.next' })}
        </Button>
      )}
    </div>
  );
}

function StoreItems({ bid, sid, editable }: { bid: string; sid: string; editable: boolean }) {
  const intl = useIntl();
  const problemText = useProblemText();
  const q = useItems(bid, sid);
  const pages = q.data?.pages ?? [];
  const items = pages.flatMap((p) => p.items);
  // The list answers whether the window is open now; the business may be a few seconds old.
  const open = editable && (pages[0]?.itemsEditable ?? true);
  return (
    <>
      {open && <AddItem bid={bid} sid={sid} />}
      {q.isError && (
        <p role="alert" className="tz-field-error">
          {problemText(q.error)}{' '}
          <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
            {intl.formatMessage({ id: 'common.retry' })}
          </Button>
        </p>
      )}
      <h3 className="tz-field-label">{intl.formatMessage({ id: 'field.items.listed' }, { n: items.length })}</h3>
      {q.isSuccess && items.length === 0 && <p className={css.muted}>{intl.formatMessage({ id: 'field.items.empty' })}</p>}
      {!open && <p className={css.muted}>{intl.formatMessage({ id: 'field.readOnly' })}</p>}
      <ul className={css.list} aria-label={intl.formatMessage({ id: 'field.items.list' })}>
        {items.map((it) => (
          // D-232: an agent changes only the items they added; the server says which.
          <ItemRow key={it.id} item={it} bid={bid} sid={sid} editable={open && it.editable} notMine={open && !it.editable} />
        ))}
      </ul>
      {q.hasNextPage && (
        <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
          {intl.formatMessage({ id: 'common.loadMore' })}
        </Button>
      )}
    </>
  );
}

function ItemRow({ item: it, bid, sid, editable, notMine }: { item: FieldItem; bid: string; sid: string; editable: boolean; notMine: boolean }) {
  const intl = useIntl();
  const problemText = useProblemText();
  const update = useUpdateItem(bid, sid);
  const [editing, setEditing] = useState(false);
  const [price, setPrice] = useState(rupees(it.pricePaise));
  const paise = toPaise(price);
  const save = (e: FormEvent) => {
    e.preventDefault();
    if (paise === null) return;
    update.mutate({ id: it.id, body: { pricePaise: paise } }, { onSuccess: () => setEditing(false) });
  };
  return (
    <li className={css.match}>
      <span>
        <span className={css.shopName}>{it.name}</span>
        <br />
        <span className={css.muted}>{[it.brand, pack(it)].filter(Boolean).join(' · ')}</span>{' '}
        {it.productStatus === 'pending_review' && <StatusPill tone="pending">{intl.formatMessage({ id: 'field.items.inReview' })}</StatusPill>}
      </span>
      {editing ? (
        <form className={css.head} onSubmit={save} aria-label={intl.formatMessage({ id: 'field.items.editPrice' }, { name: it.name })}>
          <TextField
            label={intl.formatMessage({ id: 'field.items.price' })}
            prefix="₹"
            inputMode="decimal"
            value={price}
            onChange={(e) => setPrice(e.target.value)}
            error={
              paise === null
                ? intl.formatMessage({ id: 'field.items.price.invalid' })
                : update.isError
                  ? problemText(update.error)
                  : undefined
            }
          />
          <Button type="submit" size="sm" variant="primary" loading={update.isPending}>
            {intl.formatMessage({ id: 'field.items.save' })}
          </Button>
        </form>
      ) : (
        <span className={css.head}>
          <span className={css.price}>{money(it.pricePaise)}</span>
          {notMine && <span className={css.muted}>{intl.formatMessage({ id: 'field.items.notMine' })}</span>}
          {editable && (
            <Button size="sm" variant="ghost" onClick={() => setEditing(true)} aria-label={intl.formatMessage({ id: 'field.items.editPrice' }, { name: it.name })}>
              {intl.formatMessage({ id: 'field.items.edit' })}
            </Button>
          )}
        </span>
      )}
    </li>
  );
}

function AddItem({ bid, sid }: { bid: string; sid: string }) {
  const intl = useIntl();
  const [term, setTerm] = useState('');
  const [picked, setPicked] = useState<FieldProductMatch | null>(null);
  const [creating, setCreating] = useState(false);
  const found = useProductSearch(bid, sid, term);
  const reset = () => {
    setPicked(null);
    setCreating(false);
    setTerm('');
  };
  if (picked) return <PriceForm bid={bid} sid={sid} product={picked} onDone={reset} />;
  if (creating) return <NewProductForm bid={bid} sid={sid} name={term.trim()} onDone={reset} />;
  const matches = found.data?.items ?? [];
  return (
    <div className={css.form}>
      <SearchField
        label={intl.formatMessage({ id: 'field.items.search' })}
        placeholder={intl.formatMessage({ id: 'field.items.search.hint' })}
        value={term}
        onChange={setTerm}
      />
      {term.trim().length >= 2 && (
        <ul className={css.list} aria-label={intl.formatMessage({ id: 'field.items.matches' })} aria-busy={found.isFetching || undefined}>
          {matches.map((p) => (
            <li key={p.id} className={css.match}>
              <span>
                <span className={css.shopName}>{p.name}</span>
                <br />
                <span className={css.muted}>
                  {[p.brand, pack(p), p.mrpPaise !== null ? intl.formatMessage({ id: 'field.items.mrp' }, { mrp: money(p.mrpPaise) }) : null]
                    .filter(Boolean)
                    .join(' · ')}
                </span>
              </span>
              {p.inStore ? (
                <StatusPill tone="completed">{intl.formatMessage({ id: 'field.items.already' })}</StatusPill>
              ) : (
                <Button size="sm" variant="soft" icon={Plus} onClick={() => setPicked(p)} aria-label={intl.formatMessage({ id: 'field.items.addNamed' }, { name: p.name })}>
                  {intl.formatMessage({ id: 'field.items.add' })}
                </Button>
              )}
            </li>
          ))}
          {found.isSuccess && matches.length === 0 && <li className={css.muted}>{intl.formatMessage({ id: 'field.items.noMatch' })}</li>}
        </ul>
      )}
      <Button variant="ghost" icon={Plus} onClick={() => setCreating(true)}>
        {intl.formatMessage({ id: 'field.items.new' })}
      </Button>
    </div>
  );
}

function useAddError() {
  const intl = useIntl();
  const problemText = useProblemText();
  return (err: unknown) => {
    if (err instanceof ApiError) {
      if (err.code === 'conflict') return intl.formatMessage({ id: 'field.items.already' });
      if (err.code === 'forbidden') return intl.formatMessage({ id: 'field.readOnly' });
    }
    return problemText(err);
  };
}

/** Price and opening stock for a master product. */
function PriceForm({ bid, sid, product: p, onDone }: { bid: string; sid: string; product: FieldProductMatch; onDone: () => void }) {
  const intl = useIntl();
  const toast = useToast();
  const add = useAddItem(bid, sid);
  const addError = useAddError();
  const [price, setPrice] = useState(rupees(p.mrpPaise));
  const [stock, setStock] = useState('');
  const [shown, setShown] = useState(false);
  const paise = toPaise(price);
  const opening = stock.trim() === '' ? undefined : /^\d{1,7}$/.test(stock.trim()) ? Number(stock.trim()) : null;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setShown(true);
    if (paise === null || opening === null) return;
    add.mutate(
      { productId: p.id, pricePaise: paise, ...(opening !== undefined ? { openingStock: opening } : {}) },
      {
        onSuccess: () => {
          toast.show({ message: intl.formatMessage({ id: 'field.items.added' }, { name: p.name }) });
          onDone();
        },
      },
    );
  };
  return (
    <form className={`${css.form} tz-card`} onSubmit={submit} noValidate aria-label={intl.formatMessage({ id: 'field.items.addNamed' }, { name: p.name })}>
      <div>
        <span className={css.shopName}>{p.name}</span>
        <br />
        <span className={css.muted}>{[p.brand, pack(p)].filter(Boolean).join(' · ')}</span>
      </div>
      <div className={css.row}>
        <TextField
          label={intl.formatMessage({ id: 'field.items.price' })}
          prefix="₹"
          inputMode="decimal"
          value={price}
          onChange={(e) => setPrice(e.target.value)}
          error={shown && paise === null ? intl.formatMessage({ id: 'field.items.price.invalid' }) : undefined}
        />
        <TextField
          label={intl.formatMessage({ id: 'field.items.stock' })}
          inputMode="numeric"
          value={stock}
          onChange={(e) => setStock(e.target.value)}
          error={shown && opening === null ? intl.formatMessage({ id: 'field.items.stock.invalid' }) : undefined}
        />
      </div>
      {add.isError && (
        <p role="alert" className="tz-field-error">
          {addError(add.error)}
        </p>
      )}
      <div className={css.head}>
        <Button variant="ghost" onClick={onDone}>
          {intl.formatMessage({ id: 'common.cancel' })}
        </Button>
        <Button type="submit" variant="primary" loading={add.isPending}>
          {intl.formatMessage({ id: 'field.items.add' })}
        </Button>
      </div>
    </form>
  );
}

/** A product the catalogue does not have yet: it is listed at once and waits in A-08 review. */
function NewProductForm({ bid, sid, name: initial, onDone }: { bid: string; sid: string; name: string; onDone: () => void }) {
  const intl = useIntl();
  const toast = useToast();
  const cats = useCategories();
  const add = useAddItem(bid, sid);
  const addError = useAddError();
  const t = (id: string) => intl.formatMessage({ id });
  const [v, setV] = useState({ name: initial, category: '', variant: '', packSize: '', unit: 'g' as FieldUnit, mrp: '', barcode: '', price: '', stock: '' });
  const [shown, setShown] = useState(false);
  const set = (k: keyof typeof v) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => setV((p) => ({ ...p, [k]: e.target.value }));
  const size = Number(v.packSize);
  const mrp = v.mrp.trim() ? toPaise(v.mrp) : undefined;
  const price = toPaise(v.price);
  const stock = v.stock.trim() === '' ? undefined : /^\d{1,7}$/.test(v.stock.trim()) ? Number(v.stock.trim()) : null;
  const errors = {
    name: v.name.trim() ? undefined : t('field.store.required'),
    category: v.category ? undefined : t('field.details.category.required'),
    packSize: v.packSize.trim() && Number.isFinite(size) && size > 0 && size <= 100_000 ? undefined : t('field.items.pack.invalid'),
    mrp: mrp === null ? t('field.items.price.invalid') : undefined,
    barcode: v.barcode.trim() && !/^[0-9]{8,14}$/.test(v.barcode.trim()) ? t('field.items.barcode.invalid') : undefined,
    price: price === null ? t('field.items.price.invalid') : undefined,
    stock: stock === null ? t('field.items.stock.invalid') : undefined,
  };
  const err = (k: keyof typeof errors) => (shown ? errors[k] : undefined);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setShown(true);
    if (Object.values(errors).some(Boolean) || price === null || stock === null) return;
    add.mutate(
      {
        newProduct: {
          name: v.name.trim(),
          categoryId: v.category,
          packSize: size,
          unit: v.unit,
          ...(v.variant.trim() ? { variant: v.variant.trim() } : {}),
          ...(mrp ? { mrpPaise: mrp } : {}),
          ...(v.barcode.trim() ? { barcode: v.barcode.trim() } : {}),
        },
        pricePaise: price,
        ...(stock !== undefined ? { openingStock: stock } : {}),
      },
      {
        onSuccess: () => {
          toast.show({ message: intl.formatMessage({ id: 'field.items.added' }, { name: v.name.trim() }) });
          onDone();
        },
      },
    );
  };
  return (
    <form className={`${css.form} tz-card`} onSubmit={submit} noValidate aria-label={t('field.items.new')}>
      <p className={css.muted}>{t('field.items.new.help')}</p>
      <TextField label={t('field.items.name')} value={v.name} maxLength={120} onChange={set('name')} error={err('name')} />
      <div className="tz-field">
        <label className="tz-field-label" htmlFor="field-new-category">
          {t('field.items.category')}
        </label>
        <select
          id="field-new-category"
          className={`tz-input${err('category') ? ' tz-input-invalid' : ''}`}
          value={v.category}
          onChange={set('category')}
          aria-invalid={!!err('category') || undefined}
        >
          <option value="">{t('field.details.category.pick')}</option>
          {(cats.data?.items ?? []).map((c) =>
            c.children.length > 0 ? (
              <optgroup key={c.id} label={c.name}>
                {c.children.map((ch) => (
                  <option key={ch.id} value={ch.id}>
                    {ch.name}
                  </option>
                ))}
              </optgroup>
            ) : (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ),
          )}
        </select>
        {err('category') && <p className="tz-field-error">{err('category')}</p>}
      </div>
      <TextField label={t('field.items.variant')} value={v.variant} maxLength={80} onChange={set('variant')} />
      <div className={css.row}>
        <TextField label={t('field.items.pack')} inputMode="decimal" value={v.packSize} onChange={set('packSize')} error={err('packSize')} />
        <div className="tz-field">
          <label className="tz-field-label" htmlFor="field-new-unit">
            {t('field.items.unit')}
          </label>
          <select id="field-new-unit" className="tz-input" value={v.unit} onChange={set('unit')}>
            {UNITS.map((u) => (
              <option key={u} value={u}>
                {u}
              </option>
            ))}
          </select>
        </div>
      </div>
      <div className={css.row}>
        <TextField label={t('field.items.mrpField')} prefix="₹" inputMode="decimal" value={v.mrp} onChange={set('mrp')} error={err('mrp')} />
        <TextField label={t('field.items.price')} prefix="₹" inputMode="decimal" value={v.price} onChange={set('price')} error={err('price')} />
      </div>
      <div className={css.row}>
        <TextField label={t('field.items.barcode')} inputMode="numeric" maxLength={14} value={v.barcode} onChange={set('barcode')} error={err('barcode')} />
        <TextField label={t('field.items.stock')} inputMode="numeric" value={v.stock} onChange={set('stock')} error={err('stock')} />
      </div>
      {add.isError && (
        <p role="alert" className="tz-field-error">
          {addError(add.error)}
        </p>
      )}
      <div className={css.head}>
        <Button variant="ghost" onClick={onDone}>
          {t('common.cancel')}
        </Button>
        <Button type="submit" variant="primary" loading={add.isPending}>
          {t('field.items.add')}
        </Button>
      </div>
    </form>
  );
}
