import type { ProductMatch } from '@todayz/api-client/gen/model/productMatch';
import { Unit } from '@todayz/api-client/gen/model/unit';
import { formatMoney } from '@todayz/i18n';
import { Button, SearchField, Sheet, Skeleton, TextField, useToast } from '@todayz/ui';
import { Camera } from 'lucide-react';
import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { useIdempotentAction } from './action';
import { createProduct, type CreatedProduct } from './api';
import { itemTitle, parseQuantity, parseRupees, withBrand } from './format';
import { PhotoError, shrinkPhoto, uploadProductPhoto } from './photo';
import { addItem, useCategories, useProductSearch } from './queries';
import { Icon } from '../shell/Icon';
import { StockError } from './StockError';
import { Thumb } from './Thumb';
import styles from './stock.module.css';

/** What the price step needs of a product, whether it came from the master list or was just created. */
type Chosen = Pick<ProductMatch, 'id' | 'name' | 'variant' | 'packSize' | 'unit' | 'mrpPaise'> & {
  imageKey?: string | null | undefined;
  /** Picked from the master list: the price step offers a photo (D-253). A new product took its photo already. */
  listed?: boolean;
};

export interface AddItemProps {
  store: CurrentStore;
  /** `add_q`: starts the search with this text. */
  initialQuery?: string | undefined;
  /** `add=<masterProductId>`: no endpoint reads one product by id, so this only tells the sheet a product was meant. */
  hadProductId?: boolean | undefined;
  onClose: () => void;
}

/** B-07 "Add item": find the product, or create it (D-37), then set the price and the opening stock. */
export function AddItem({ store, initialQuery = '', hadProductId = false, onClose }: AddItemProps) {
  const intl = useIntl();
  const [q, setQ] = useState(initialQuery);
  const [chosen, setChosen] = useState<Chosen | null>(null);
  const [creating, setCreating] = useState(false);
  const barcode = /^\d{8,14}$/.test(q.trim()) ? q.trim() : undefined;
  const matches = useProductSearch(store, q, barcode);

  return (
    <Sheet open onClose={onClose} title={intl.formatMessage({ id: 'stock.add.title' })} closeLabel={intl.formatMessage({ id: 'stock.item.close' })}>
      <div className={styles.sheetBody}>
        {chosen ? (
          <PriceStep store={store} product={chosen} onBack={() => setChosen(null)} onDone={onClose} />
        ) : creating ? (
          <NewProductForm
            store={store}
            initialName={barcode ? '' : q.trim()}
            initialBarcode={barcode}
            onCreated={(p) => {
              setCreating(false);
              setChosen(p);
            }}
            onCancel={() => setCreating(false)}
          />
        ) : (
          <>
            {hadProductId && <p className={styles.note}>{intl.formatMessage({ id: 'stock.add.pickAgain' })}</p>}
            <SearchField
              label={intl.formatMessage({ id: 'stock.add.search' })}
              placeholder={intl.formatMessage({ id: 'stock.add.searchHint' })}
              value={q}
              onChange={setQ}
              clearLabel={intl.formatMessage({ id: 'stock.search.clear' })}
            />
            <Matches query={matches} enabled={q.trim().length >= 2 || barcode !== undefined} onPick={(p) => setChosen({ ...p, name: withBrand(p.name, p.brand), listed: true })} />
            <div className={styles.actions}>
              <Button onClick={() => setCreating(true)}>{intl.formatMessage({ id: 'stock.add.create' })}</Button>
            </div>
          </>
        )}
      </div>
    </Sheet>
  );
}

function Matches({ query, enabled, onPick }: { query: ReturnType<typeof useProductSearch>; enabled: boolean; onPick: (p: ProductMatch) => void }) {
  const intl = useIntl();
  if (!enabled) return <p className={styles.sub}>{intl.formatMessage({ id: 'stock.add.hint' })}</p>;
  if (query.isError) return <p role="alert" className="tz-field-error">{intl.formatMessage({ id: 'stock.error.generic' })}</p>;
  if (!query.data) return <Skeleton height="3rem" />;
  if (query.data.items.length === 0) return <p className={styles.sub}>{intl.formatMessage({ id: 'stock.add.noMatch' })}</p>;
  return (
    <ul className={styles.matches} aria-label={intl.formatMessage({ id: 'stock.add.results' })}>
      {query.data.items.map((p) => (
        <li key={p.id} className={styles.match}>
          <Thumb item={{ name: withBrand(p.name, p.brand), imageKey: p.imageKey ?? null }} />
          <span className={styles.cell}>
            <span className={styles.name}>{itemTitle({ name: withBrand(p.name, p.brand), variant: p.variant, packSize: p.packSize, unit: p.unit })}</span>
            {p.mrpPaise !== null && <span className={styles.sub}>{intl.formatMessage({ id: 'stock.item.mrp' })} {formatMoney(p.mrpPaise, 'product')}</span>}
          </span>
          {p.inStore ? (
            <span className={styles.sub}>{intl.formatMessage({ id: 'stock.add.inStore' })}</span>
          ) : (
            <Button size="sm" variant="primary" onClick={() => onPick(p)}>
              {intl.formatMessage({ id: 'stock.add.pick' })}
            </Button>
          )}
        </li>
      ))}
    </ul>
  );
}

function PriceStep({ store, product, onBack, onDone }: { store: CurrentStore; product: Chosen; onBack: () => void; onDone: () => void }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const action = useIdempotentAction();
  const [price, setPrice] = useState('');
  const [opening, setOpening] = useState('');
  const [expiresOn, setExpiresOn] = useState('');
  const [invalid, setInvalid] = useState<{ price?: string; opening?: string }>({});
  const photo = usePhoto();
  const [uploading, setUploading] = useState(false);
  const title = itemTitle(product);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const paise = parseRupees(price);
    const errors: { price?: string; opening?: string } = {};
    if (paise === null) errors.price = 'stock.form.priceInvalid';
    const stock = opening.trim() === '' ? 0 : parseQuantity(opening);
    if (stock === null) errors.opening = 'stock.form.countInvalid';
    setInvalid(errors);
    if (paise === null || stock === null || errors.price) return;
    const body = {
      productId: product.id,
      pricePaise: paise,
      ...(stock > 0 ? { openingStock: stock, ...(expiresOn ? { expiresOn } : {}) } : {}),
    };
    const out = await action.run(JSON.stringify(body), (key) => addItem(qc, store, body, key));
    if (!out) return;
    toast.show({ message: intl.formatMessage({ id: 'stock.toast.added' }, { item: title }) });
    if (photo.blob) {
      // The item is listed either way; a photo that does not upload only costs the picture.
      setUploading(true);
      try {
        await uploadProductPhoto(store, product.id, photo.blob);
        toast.show({ message: intl.formatMessage({ id: 'stock.photo.sent' }) });
      } catch {
        toast.show({ message: intl.formatMessage({ id: 'stock.photo.failed' }) });
      } finally {
        setUploading(false);
      }
    }
    onDone();
  };

  return (
    <form className={styles.form} onSubmit={(e) => void submit(e)} noValidate aria-label={intl.formatMessage({ id: 'stock.add.priceTitle' })}>
      <div className={styles.photoField}>
        {product.listed && <Thumb item={{ name: product.name, imageKey: product.imageKey ?? null }} large />}
        <h3 className={styles.formTitle}>{title}</h3>
      </div>
      <TextField
        label={intl.formatMessage({ id: 'stock.form.price' })}
        help={product.mrpPaise !== null ? intl.formatMessage({ id: 'stock.form.priceMrp' }, { mrp: formatMoney(product.mrpPaise, 'product') }) : undefined}
        error={invalid.price ? intl.formatMessage({ id: invalid.price }, { mrp: product.mrpPaise === null ? '' : formatMoney(product.mrpPaise, 'product') }) : undefined}
        inputMode="decimal"
        prefix="₹"
        value={price}
        onChange={(e) => setPrice(e.target.value)}
      />
      <TextField
        label={intl.formatMessage({ id: 'stock.add.opening' })}
        help={intl.formatMessage({ id: 'stock.add.openingHelp' })}
        error={invalid.opening ? intl.formatMessage({ id: invalid.opening }) : undefined}
        inputMode="numeric"
        value={opening}
        onChange={(e) => setOpening(e.target.value)}
      />
      <TextField label={intl.formatMessage({ id: 'stock.form.expiry' })} type="date" value={expiresOn} onChange={(e) => setExpiresOn(e.target.value)} />
      {product.listed && <PhotoPicker photo={photo} />}
      <StockError error={action.error} />
      <div className={styles.formActions}>
        <Button onClick={onBack}>{intl.formatMessage({ id: 'stock.form.back' })}</Button>
        <Button type="submit" variant="primary" loading={action.pending || uploading}>
          {intl.formatMessage({ id: 'stock.add.save' })}
        </Button>
      </div>
    </form>
  );
}

/** A product the master list does not have. It sells at once and TodayZ reviews it later (D-37). */
function NewProductForm({
  store,
  initialName,
  initialBarcode,
  onCreated,
  onCancel,
}: {
  store: CurrentStore;
  initialName: string;
  initialBarcode: string | undefined;
  onCreated: (p: CreatedProduct) => void;
  onCancel: () => void;
}) {
  const intl = useIntl();
  const toast = useToast();
  const action = useIdempotentAction();
  const categories = useCategories();
  const [name, setName] = useState(initialName);
  const [brand, setBrand] = useState('');
  const [packSize, setPackSize] = useState('1');
  const [unit, setUnit] = useState<Unit>(Unit.pc);
  const [mrp, setMrp] = useState('');
  const [categoryId, setCategoryId] = useState('');
  const [barcode, setBarcode] = useState(initialBarcode ?? '');
  const [invalid, setInvalid] = useState<Record<string, string>>({});
  const photo = usePhoto();
  const [uploading, setUploading] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const errors: Record<string, string> = {};
    const pack = Number(packSize);
    if (name.trim().length < 2) errors.name = 'stock.add.nameInvalid';
    if (!(pack > 0) || pack > 100_000) errors.pack = 'stock.add.packInvalid';
    if (!categoryId) errors.category = 'stock.add.categoryInvalid';
    const mrpPaise = mrp.trim() === '' ? undefined : parseRupees(mrp);
    if (mrp.trim() !== '' && mrpPaise === null) errors.mrp = 'stock.form.priceInvalid';
    if (barcode.trim() !== '' && !/^\d{8,14}$/.test(barcode.trim())) errors.barcode = 'stock.add.barcodeInvalid';
    setInvalid(errors);
    if (Object.keys(errors).length > 0) return;
    const body = {
      storeId: store.sid,
      name: name.trim(),
      ...(brand.trim() ? { brand: brand.trim() } : {}),
      packSize: pack,
      unit,
      categoryId,
      ...(mrpPaise ? { mrpPaise } : {}),
      ...(barcode.trim() ? { barcodes: [barcode.trim()] } : {}),
    };
    const out = await action.run(JSON.stringify(body), (key) => createProduct(store.bid, body, key));
    if (!out) return;
    if (photo.blob) {
      // The product is made either way; a photo that does not upload only costs the picture.
      setUploading(true);
      try {
        await uploadProductPhoto(store, out.id, photo.blob);
        toast.show({ message: intl.formatMessage({ id: 'stock.photo.sent' }) });
      } catch {
        toast.show({ message: intl.formatMessage({ id: 'stock.photo.failed' }) });
      } finally {
        setUploading(false);
      }
    }
    onCreated(out);
  };

  const err = (k: string) => (invalid[k] ? intl.formatMessage({ id: invalid[k] }) : undefined);
  const leaves = (categories.data?.items ?? []).flatMap((c) => (c.children.length > 0 ? c.children : [c]));

  return (
    <form className={styles.form} onSubmit={(e) => void submit(e)} noValidate aria-label={intl.formatMessage({ id: 'stock.add.createTitle' })}>
      <h3 className={styles.formTitle}>{intl.formatMessage({ id: 'stock.add.createTitle' })}</h3>
      <p className={styles.note}>{intl.formatMessage({ id: 'stock.add.createNote' })}</p>
      <TextField label={intl.formatMessage({ id: 'stock.add.name' })} error={err('name')} value={name} onChange={(e) => setName(e.target.value)} maxLength={120} />
      <TextField label={intl.formatMessage({ id: 'stock.add.brand' })} value={brand} onChange={(e) => setBrand(e.target.value)} maxLength={60} />
      <TextField label={intl.formatMessage({ id: 'stock.add.packSize' })} error={err('pack')} inputMode="decimal" value={packSize} onChange={(e) => setPackSize(e.target.value)} />
      <div className="tz-field">
        <label className="tz-field-label" htmlFor="stock-add-unit">
          {intl.formatMessage({ id: 'stock.add.unit' })}
        </label>
        <select id="stock-add-unit" className="tz-input" value={unit} onChange={(e) => setUnit(e.target.value as Unit)}>
          {Object.values(Unit).map((u) => (
            <option key={u} value={u}>
              {u}
            </option>
          ))}
        </select>
      </div>
      <div className="tz-field">
        <label className="tz-field-label" htmlFor="stock-add-category">
          {intl.formatMessage({ id: 'stock.add.category' })}
        </label>
        <select
          id="stock-add-category"
          className="tz-input"
          value={categoryId}
          aria-invalid={invalid.category ? true : undefined}
          onChange={(e) => setCategoryId(e.target.value)}
        >
          <option value="">{intl.formatMessage({ id: 'stock.add.categoryPick' })}</option>
          {leaves.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name}
            </option>
          ))}
        </select>
        {invalid.category && <span className="tz-field-error">{err('category')}</span>}
      </div>
      <TextField label={intl.formatMessage({ id: 'stock.add.mrp' })} error={err('mrp')} inputMode="decimal" prefix="₹" value={mrp} onChange={(e) => setMrp(e.target.value)} />
      <TextField label={intl.formatMessage({ id: 'stock.add.barcode' })} error={err('barcode')} inputMode="numeric" value={barcode} onChange={(e) => setBarcode(e.target.value)} />
      <PhotoPicker photo={photo} />
      <StockError error={action.error} />
      <div className={styles.formActions}>
        <Button onClick={onCancel}>{intl.formatMessage({ id: 'stock.form.back' })}</Button>
        <Button type="submit" variant="primary" loading={action.pending || uploading}>
          {intl.formatMessage({ id: 'stock.add.createSave' })}
        </Button>
      </div>
    </form>
  );
}

type Photo = ReturnType<typeof usePhoto>;

/** The optional photo of a product (D-252, D-253): shrunk on the device as soon as it is picked. */
function usePhoto() {
  const [blob, setBlob] = useState<Blob | null>(null);
  const [preview, setPreview] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(
    () => () => {
      if (preview) URL.revokeObjectURL(preview);
    },
    [preview],
  );

  const pick = async (file: File | undefined) => {
    if (!file) return;
    setBusy(true);
    setError(null);
    try {
      const out = await shrinkPhoto(file);
      setBlob(out);
      setPreview(typeof URL.createObjectURL === 'function' ? URL.createObjectURL(out) : null);
    } catch (e) {
      setBlob(null);
      setPreview(null);
      setError(e instanceof PhotoError && e.reason === 'size' ? 'stock.photo.tooBig' : 'stock.photo.badType');
    } finally {
      setBusy(false);
    }
  };
  const clear = () => {
    setBlob(null);
    setPreview(null);
    setError(null);
  };
  return { blob, preview, error, busy, pick, clear };
}

function PhotoPicker({ photo }: { photo: Photo }) {
  const intl = useIntl();
  return (
    <div className="tz-field">
      <span className="tz-field-label">{intl.formatMessage({ id: 'stock.photo.label' })}</span>
      <div className={styles.photoField}>
        {photo.blob && (
          <span className={`${styles.thumb} ${styles.thumbLarge}`}>
            {photo.preview && <img src={photo.preview} alt={intl.formatMessage({ id: 'stock.photo.preview' })} />}
          </span>
        )}
        <div className={styles.photoActions}>
          <input
            id="stock-add-photo"
            type="file"
            accept="image/*"
            capture="environment"
            className={styles.fileInput}
            disabled={photo.busy}
            onChange={(e) => {
              void photo.pick(e.target.files?.[0]);
              e.target.value = '';
            }}
          />
          <label htmlFor="stock-add-photo" className="tz-btn tz-btn-sm">
            <Icon icon={Camera} />
            {intl.formatMessage({ id: photo.blob ? 'stock.photo.change' : 'stock.photo.add' })}
          </label>
          {photo.blob && (
            <Button size="sm" variant="ghost" onClick={photo.clear}>
              {intl.formatMessage({ id: 'stock.photo.remove' })}
            </Button>
          )}
        </div>
      </div>
      {photo.error ? (
        <span className="tz-field-error" role="alert">
          {intl.formatMessage({ id: photo.error })}
        </span>
      ) : (
        <span className="tz-field-help">{intl.formatMessage({ id: 'stock.photo.help' })}</span>
      )}
    </div>
  );
}
