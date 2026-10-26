import type { AdminProduct } from '@todayz/api-client/gen/model/adminProduct';
import type { AdminProductStatus } from '@todayz/api-client/gen/model/adminProductStatus';
import { Button, DataTable, StatusPill, Tabs, TextField, useToast, type ColumnDef } from '@todayz/ui';
import { useNavigate } from '@tanstack/react-router';
import { ImagePlus, Pencil, Plus } from 'lucide-react';
import { useMemo, useRef, useState, type ChangeEvent, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { can, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { AuditPanel } from '../audit/AuditPanel';
import { CategoriesTab } from './CategoriesTab';
import { imageUrl } from './image';
import { ImagesTab } from './ImagesTab';
import { ProductDialog, type ProductSave } from './ProductDialog';
import { ReviewTab } from './ReviewTab';
import { SynonymsTab } from './SynonymsTab';
import { TypePhotosTab } from './TypePhotosTab';
import { PRODUCT_STATUSES, productTone, useCatalogueLabels } from './labels';
import { useCatalogueProblem } from './problem';
import { ProductThumb } from './ProductThumb';
import {
  IMAGE_MAX_BYTES,
  IMAGE_TYPES,
  UploadError,
  useCategories,
  useCreateProduct,
  useProduct,
  useProducts,
  useUpdateProduct,
  useUploadImage,
} from './queries';
import { MAX_Q, type CatalogueSearch, type CatalogueTab, type SubTab } from './search';
import css from '../app/page.module.css';
import img from './images.module.css';

export type Go = (next: CatalogueSearch) => void;

/** A-08 Master catalogue: products, the review queue, categories and search synonyms (04 §2.7). */
export function CataloguePage({ search }: { search: CatalogueSearch }) {
  const intl = useIntl();
  const navigate = useNavigate();
  const go: Go = (next) => void navigate({ to: '/catalogue', search: next });
  const tab: CatalogueTab = search.tab ?? 'products';
  return (
    <div className={css.column}>
      <div>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.catalogue' })}</h1>
        <p className="tz-page-sub">{intl.formatMessage({ id: 'catalogue.sub' })}</p>
      </div>
      <Tabs
        label={intl.formatMessage({ id: 'catalogue.tabs' })}
        tabs={[
          { value: 'products', label: intl.formatMessage({ id: 'catalogue.tab.products' }) },
          { value: 'review', label: intl.formatMessage({ id: 'catalogue.tab.review' }) },
          { value: 'images', label: intl.formatMessage({ id: 'catalogue.tab.images' }) },
          { value: 'types', label: intl.formatMessage({ id: 'catalogue.tab.types' }) },
          { value: 'categories', label: intl.formatMessage({ id: 'catalogue.tab.categories' }) },
          { value: 'synonyms', label: intl.formatMessage({ id: 'catalogue.tab.synonyms' }) },
        ]}
        value={tab}
        onChange={(v) => go(v === 'products' ? {} : { tab: v as SubTab })}
      />
      {tab === 'products' && <ProductsTab search={search} go={go} />}
      {tab === 'review' && <ReviewTab search={search} go={go} />}
      {tab === 'images' && <ImagesTab search={search} go={go} />}
      {tab === 'types' && <TypePhotosTab search={search} go={go} />}
      {tab === 'categories' && <CategoriesTab search={search} go={go} />}
      {tab === 'synonyms' && <SynonymsTab search={search} go={go} />}
    </div>
  );
}

function ProductsTab({ search, go }: { search: CatalogueSearch; go: Go }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoles();
  const labels = useCatalogueLabels();
  const problemText = useProblemText();
  const catalogueProblem = useCatalogueProblem();
  const manage = can(roles, 'catalogueManage');
  const filter = useMemo(
    () => ({
      ...(search.q ? { q: search.q } : {}),
      ...(search.status ? { status: search.status } : {}),
      ...(search.categoryId ? { categoryId: search.categoryId } : {}),
    }),
    [search.q, search.status, search.categoryId],
  );
  const q = useProducts(filter);
  const categories = useCategories().data?.items ?? [];
  const create = useCreateProduct();
  const [adding, setAdding] = useState(false);
  const pages = q.data?.pages ?? [];
  const rows = pages.flatMap((p) => p.items);
  const now = pages[0]?.serverNow;
  const listed = search.open ? rows.find((r) => r.id === search.open) : undefined;
  // A product opened from a link (the review queue, the audit log) may not be on the loaded pages.
  const one = useProduct(search.open, !listed && !q.isPending);
  const selected = listed ?? one.data;

  const columns = useMemo<ColumnDef<AdminProduct, unknown>[]>(
    () => [
      {
        id: 'name',
        header: intl.formatMessage({ id: 'catalogue.col.product' }),
        cell: ({ row }) => (
          <span className={img.nameCell}>
            <ProductThumb name={row.original.name} src={imageUrl(row.original.imageKey)} />
            <Button size="sm" variant="ghost" aria-pressed={row.original.id === search.open} onClick={() => go({ ...search, open: row.original.id })}>
              {labels.name(row.original)}
            </Button>
          </span>
        ),
      },
      { id: 'brand', header: intl.formatMessage({ id: 'catalogue.col.brand' }), accessorFn: (r) => r.brandName ?? '' },
      { id: 'category', header: intl.formatMessage({ id: 'catalogue.col.category' }), accessorFn: (r) => r.categoryName },
      { id: 'pack', header: intl.formatMessage({ id: 'catalogue.col.pack' }), accessorFn: (r) => labels.pack(r), meta: { numeric: true } },
      { id: 'mrp', header: intl.formatMessage({ id: 'catalogue.col.mrp' }), accessorFn: (r) => labels.mrp(r.mrpPaise), meta: { numeric: true } },
      { id: 'stores', header: intl.formatMessage({ id: 'catalogue.col.stores' }), accessorFn: (r) => r.storeCount, meta: { numeric: true } },
      {
        id: 'status',
        header: intl.formatMessage({ id: 'catalogue.col.status' }),
        cell: ({ row }) => <StatusPill tone={productTone(row.original.status)}>{labels.status(row.original.status)}</StatusPill>,
      },
      { id: 'updated', header: intl.formatMessage({ id: 'catalogue.col.updated' }), accessorFn: (r) => when(r.updatedAt, now ?? r.updatedAt) },
    ],
    [intl, labels, when, now, search, go],
  );

  const added = (save: ProductSave) => {
    if (save.kind !== 'create') return;
    create.mutate(save.body, {
      onSuccess: (p) => {
        setAdding(false);
        toast.show({ message: intl.formatMessage({ id: 'catalogue.product.added' }) });
        go({ ...search, open: p.id });
      },
    });
  };

  return (
    <div className={css.detail}>
      <div className={css.column}>
        <div className={css.head}>
          <ProductFilters key={`${search.q ?? ''}:${search.status ?? ''}:${search.categoryId ?? ''}`} search={search} onApply={go} />
          {manage && (
            <Button
              variant="primary"
              icon={Plus}
              onClick={() => {
                create.reset();
                setAdding(true);
              }}
            >
              {intl.formatMessage({ id: 'catalogue.product.add' })}
            </Button>
          )}
        </div>
        {q.isError ? (
          <p role="alert" className="tz-field-error">
            {problemText(q.error)}{' '}
            <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
              {intl.formatMessage({ id: 'common.retry' })}
            </Button>
          </p>
        ) : (
          <DataTable
            label={intl.formatMessage({ id: 'catalogue.products.table' })}
            columns={columns}
            data={rows}
            getRowId={(r) => r.id}
            loading={q.isPending}
            emptyLabel={intl.formatMessage({ id: 'catalogue.products.empty' })}
            footer={
              q.hasNextPage ? (
                <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
                  {intl.formatMessage({ id: 'common.loadMore' })}
                </Button>
              ) : undefined
            }
          />
        )}
      </div>
      <div className={css.column}>
        {selected ? (
          <ProductCard key={selected.id} p={selected} now={now ?? selected.updatedAt} go={go} />
        ) : (
          <p className={`tz-card ${css.muted}`}>
            {intl.formatMessage({ id: search.open && one.isError ? 'catalogue.pick.missing' : 'catalogue.pick.product' })}
          </p>
        )}
      </div>
      {adding && (
        <ProductDialog
          categories={categories}
          pending={create.isPending}
          error={create.error ? catalogueProblem(create.error, 'barcode') : null}
          onSave={added}
          onClose={() => setAdding(false)}
        />
      )}
    </div>
  );
}

function ProductFilters({ search, onApply }: { search: CatalogueSearch; onApply: Go }) {
  const intl = useIntl();
  const labels = useCatalogueLabels();
  const categories = useCategories().data?.items ?? [];
  const [q, setQ] = useState(search.q ?? '');
  const [status, setStatus] = useState<AdminProductStatus | ''>(search.status ?? '');
  const [categoryId, setCategoryId] = useState(search.categoryId ?? '');
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const t = q.trim();
    onApply({
      ...(t ? { q: t } : {}),
      ...(status ? { status } : {}),
      ...(categoryId ? { categoryId } : {}),
    });
  };
  return (
    <form className={css.filters} onSubmit={submit} role="search" aria-label={intl.formatMessage({ id: 'catalogue.filters' })}>
      <TextField
        label={intl.formatMessage({ id: 'catalogue.filter.q' })}
        help={intl.formatMessage({ id: 'catalogue.filter.qHelp' })}
        value={q}
        maxLength={MAX_Q}
        autoComplete="off"
        onChange={(e) => setQ(e.target.value)}
      />
      <div className="tz-field">
        <label className="tz-field-label" htmlFor="catalogue-status">
          {intl.formatMessage({ id: 'catalogue.filter.status' })}
        </label>
        <select id="catalogue-status" className="tz-input" value={status} onChange={(e) => setStatus(e.target.value as AdminProductStatus | '')}>
          <option value="">{intl.formatMessage({ id: 'catalogue.filter.anyStatus' })}</option>
          {PRODUCT_STATUSES.map((s) => (
            <option key={s} value={s}>
              {labels.status(s)}
            </option>
          ))}
        </select>
      </div>
      <div className="tz-field">
        <label className="tz-field-label" htmlFor="catalogue-category">
          {intl.formatMessage({ id: 'catalogue.filter.category' })}
        </label>
        <select id="catalogue-category" className="tz-input" value={categoryId} onChange={(e) => setCategoryId(e.target.value)}>
          <option value="">{intl.formatMessage({ id: 'catalogue.filter.anyCategory' })}</option>
          {categories.map((c) => (
            <option key={c.id} value={c.id}>
              {labels.category(c, categories)}
            </option>
          ))}
        </select>
      </div>
      <div className={css.actions}>
        <Button type="submit" variant="primary">
          {intl.formatMessage({ id: 'catalogue.filter.apply' })}
        </Button>
        <Button type="button" variant="ghost" onClick={() => onApply({})}>
          {intl.formatMessage({ id: 'catalogue.filter.clear' })}
        </Button>
      </div>
    </form>
  );
}

/** The chosen product: its facts, the edit an admin may make, and its history. */
function ProductCard({ p, now, go }: { p: AdminProduct; now: string; go: Go }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoles();
  const labels = useCatalogueLabels();
  const catalogueProblem = useCatalogueProblem();
  const categories = useCategories().data?.items ?? [];
  const update = useUpdateProduct(p.id);
  const [editing, setEditing] = useState(false);
  const none = intl.formatMessage({ id: 'catalogue.none' });
  // A merged product is final (409 from the API), and a pending one is decided in the queue.
  const editable = can(roles, 'catalogueManage') && p.status !== 'merged';

  return (
    <>
      <section className="tz-card" aria-labelledby={`product-${p.id}`}>
        <h2 id={`product-${p.id}`} className="tz-card-title">
          {labels.name(p)}
        </h2>
        <ProductImage p={p} />
        <dl className={css.facts}>
          <dt>{intl.formatMessage({ id: 'catalogue.col.brand' })}</dt>
          <dd>{p.brandName ?? none}</dd>
          <dt>{intl.formatMessage({ id: 'catalogue.col.category' })}</dt>
          <dd>{p.categoryName}</dd>
          <dt>{intl.formatMessage({ id: 'catalogue.col.pack' })}</dt>
          <dd className={css.mono}>{labels.pack(p)}</dd>
          <dt>{intl.formatMessage({ id: 'catalogue.col.mrp' })}</dt>
          <dd className={css.mono}>{labels.mrp(p.mrpPaise)}</dd>
          <dt>{intl.formatMessage({ id: 'catalogue.field.barcodes' })}</dt>
          <dd className={css.mono}>{p.barcodes.length ? p.barcodes.join(', ') : none}</dd>
          <dt>{intl.formatMessage({ id: 'catalogue.col.stores' })}</dt>
          <dd className={css.mono}>{p.storeCount}</dd>
          <dt>{intl.formatMessage({ id: 'catalogue.col.status' })}</dt>
          <dd>
            <StatusPill tone={productTone(p.status)}>{labels.status(p.status)}</StatusPill>
            {p.mergedInto && (
              <p>
                <Button size="sm" variant="ghost" onClick={() => p.mergedInto && go({ open: p.mergedInto })}>
                  {intl.formatMessage({ id: 'catalogue.product.mergedInto' })}
                </Button>
              </p>
            )}
          </dd>
          <dt>{intl.formatMessage({ id: 'catalogue.col.added' })}</dt>
          <dd>{when(p.createdAt, now)}</dd>
          <dt>{intl.formatMessage({ id: 'catalogue.col.updated' })}</dt>
          <dd>{when(p.updatedAt, now)}</dd>
        </dl>
        {p.status === 'pending_review' && (
          <p className={css.muted}>
            {intl.formatMessage({ id: 'catalogue.product.inQueue' })}{' '}
            <Button size="sm" variant="ghost" onClick={() => go({ tab: 'review', open: p.id })}>
              {intl.formatMessage({ id: 'catalogue.product.openQueue' })}
            </Button>
          </p>
        )}
        {editable && (
          <div className={css.actions}>
            <Button
              icon={Pencil}
              onClick={() => {
                update.reset();
                setEditing(true);
              }}
            >
              {intl.formatMessage({ id: 'catalogue.product.edit' })}
            </Button>
          </div>
        )}
      </section>
      <AuditPanel type="product" id={p.id} />
      {editing && (
        <ProductDialog
          product={p}
          categories={categories}
          pending={update.isPending}
          error={update.error ? catalogueProblem(update.error, 'barcode') : null}
          onSave={(save) => {
            if (save.kind !== 'update') return;
            update.mutate(save.body, {
              onSuccess: () => {
                setEditing(false);
                toast.show({ message: intl.formatMessage({ id: 'catalogue.product.saved' }) });
              },
            });
          }}
          onClose={() => setEditing(false)}
        />
      )}
    </>
  );
}

/**
 * The product's image and, for an admin, upload or replace it (D-252). An admin's image needs no
 * review: confirming it publishes it at once. A merged or retired product keeps what it has.
 */
function ProductImage({ p }: { p: AdminProduct }) {
  const intl = useIntl();
  const toast = useToast();
  const roles = useRoles();
  const catalogueProblem = useCatalogueProblem();
  const upload = useUploadImage(p.id);
  const input = useRef<HTMLInputElement>(null);
  const [error, setError] = useState<string | null>(null);
  const src = imageUrl(p.imageKey);
  const may = can(roles, 'catalogueManage') && (p.status === 'active' || p.status === 'pending_review');

  const chosen = (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file) return;
    if (!IMAGE_TYPES.includes(file.type)) return setError(intl.formatMessage({ id: 'catalogue.product.badType' }));
    if (file.size > IMAGE_MAX_BYTES) return setError(intl.formatMessage({ id: 'catalogue.product.tooBig' }));
    setError(null);
    upload.mutate(file, {
      onSuccess: () => toast.show({ message: intl.formatMessage({ id: 'catalogue.product.uploaded' }) }),
      onError: (err) => setError(err instanceof UploadError ? intl.formatMessage({ id: 'catalogue.product.uploadFailed' }) : catalogueProblem(err)),
    });
  };

  return (
    <div className={img.figure}>
      <ProductThumb large name={p.name} src={src} {...(src ? { alt: intl.formatMessage({ id: 'catalogue.images.altCurrent' }, { name: p.name }) } : {})} />
      {!src && <p className={css.muted}>{intl.formatMessage({ id: 'catalogue.product.noImage' })}</p>}
      {may && (
        <>
          <input
            ref={input}
            className={img.fileInput}
            type="file"
            accept={IMAGE_TYPES.join(',')}
            tabIndex={-1}
            aria-hidden="true"
            onChange={chosen}
            data-testid="catalogue-image-input"
          />
          <div className={css.actions}>
            <Button size="sm" variant="soft" icon={ImagePlus} loading={upload.isPending} onClick={() => input.current?.click()}>
              {intl.formatMessage({ id: src ? 'catalogue.product.replace' : 'catalogue.product.upload' })}
            </Button>
          </div>
          {error ? (
            <p role="alert" className="tz-field-error">
              {error}
            </p>
          ) : (
            <p className={css.muted}>{intl.formatMessage({ id: 'catalogue.product.uploadHelp' })}</p>
          )}
        </>
      )}
    </div>
  );
}
