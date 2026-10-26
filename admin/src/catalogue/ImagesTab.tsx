import type { AdminProductImage } from '@todayz/api-client/gen/model/adminProductImage';
import type { AdminProductImageBulkFailure } from '@todayz/api-client/gen/model/adminProductImageBulkFailure';
import type { AdminProductImageSource } from '@todayz/api-client/gen/model/adminProductImageSource';
import { Button, DataTable, useToast, type ColumnDef } from '@todayz/ui';
import { Check, X } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { can, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { ReasonDialog } from '../app/ReasonDialog';
import { AuditPanel } from '../audit/AuditPanel';
import type { Go } from './CataloguePage';
import { IMAGE_SOURCES, imageUrl, useImageLabels } from './image';
import { ProductThumb } from './ProductThumb';
import { useCatalogueProblem } from './problem';
import { useApproveImages, useDecideImage, useImages, type ImageDecision } from './queries';
import type { CatalogueSearch } from './search';
import css from '../app/page.module.css';
import img from './images.module.css';

/**
 * The image queue (D-252): shop photos and images imported from the open databases wait here as
 * drafts until someone approves them; only then do customers see them. Oldest first.
 */
export function ImagesTab({ search, go }: { search: CatalogueSearch; go: Go }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoles();
  const labels = useImageLabels();
  const problemText = useProblemText();
  const catalogueProblem = useCatalogueProblem();
  const may = can(roles, 'catalogueReview');
  const q = useImages(search.source);
  const bulk = useApproveImages();
  const pages = q.data?.pages;
  const rows = useMemo(() => (pages ?? []).flatMap((p) => p.items), [pages]);
  const now = pages?.[0]?.serverNow;
  const selected = search.open ? rows.find((r) => r.id === search.open) : undefined;
  const [picked, setPicked] = useState<ReadonlySet<string>>(new Set());
  const [asking, setAsking] = useState(false);
  const [failed, setFailed] = useState<AdminProductImageBulkFailure[]>([]);
  // Only rows still on the list count; a decided image drops out of the choice by itself.
  const chosen = rows.filter((r) => picked.has(r.id)).map((r) => r.id);
  const none = intl.formatMessage({ id: 'catalogue.none' });

  const columns = useMemo<ColumnDef<AdminProductImage, unknown>[]>(() => {
    const all = rows.length > 0 && rows.every((r) => picked.has(r.id));
    const cols: ColumnDef<AdminProductImage, unknown>[] = [
      {
        id: 'image',
        header: intl.formatMessage({ id: 'catalogue.images.col.image' }),
        cell: ({ row }) => <ProductThumb name={row.original.productName} src={row.original.previewUrl ?? undefined} />,
      },
      {
        id: 'product',
        header: intl.formatMessage({ id: 'catalogue.col.product' }),
        cell: ({ row }) => (
          <Button size="sm" variant="ghost" aria-pressed={row.original.id === search.open} onClick={() => go({ ...search, open: row.original.id })}>
            {row.original.productName}
          </Button>
        ),
      },
      { id: 'source', header: intl.formatMessage({ id: 'catalogue.images.col.source' }), accessorFn: (r) => labels.source(r.source) },
      { id: 'match', header: intl.formatMessage({ id: 'catalogue.images.col.match' }), accessorFn: (r) => labels.match(r.match) ?? none },
      { id: 'store', header: intl.formatMessage({ id: 'catalogue.col.store' }), accessorFn: (r) => r.storeName ?? none },
      { id: 'added', header: intl.formatMessage({ id: 'catalogue.col.added' }), accessorFn: (r) => when(r.createdAt, now ?? r.createdAt) },
    ];
    if (!may) return cols;
    return [
      {
        id: 'pick',
        header: () => (
          <input
            type="checkbox"
            aria-label={intl.formatMessage({ id: 'catalogue.images.pickAll' })}
            checked={all}
            disabled={rows.length === 0}
            onChange={(e) => setPicked(new Set(e.target.checked ? rows.map((r) => r.id) : []))}
          />
        ),
        cell: ({ row }) => (
          <input
            type="checkbox"
            aria-label={intl.formatMessage({ id: 'catalogue.images.pick' }, { name: row.original.productName })}
            checked={picked.has(row.original.id)}
            onChange={(e) =>
              setPicked((cur) => {
                const next = new Set(cur);
                if (e.target.checked) next.add(row.original.id);
                else next.delete(row.original.id);
                return next;
              })
            }
          />
        ),
      },
      ...cols,
    ];
  }, [intl, labels, when, now, none, rows, picked, may, search, go]);

  const nameOf = (id: string) => rows.find((r) => r.id === id)?.productName ?? id;

  return (
    <div className={css.detail}>
      <div className={css.column}>
        <div className={css.head}>
          <div className="tz-field">
            <label className="tz-field-label" htmlFor="catalogue-image-source">
              {intl.formatMessage({ id: 'catalogue.images.filter.source' })}
            </label>
            <select
              id="catalogue-image-source"
              className="tz-input"
              value={search.source ?? ''}
              onChange={(e) => go({ tab: 'images', ...(e.target.value ? { source: e.target.value as AdminProductImageSource } : {}) })}
            >
              <option value="">{intl.formatMessage({ id: 'catalogue.images.filter.anySource' })}</option>
              {IMAGE_SOURCES.map((s) => (
                <option key={s} value={s}>
                  {labels.source(s)}
                </option>
              ))}
            </select>
          </div>
          {may && (
            <Button
              variant="primary"
              icon={Check}
              disabled={chosen.length === 0}
              onClick={() => {
                bulk.reset();
                setAsking(true);
              }}
            >
              {intl.formatMessage({ id: 'catalogue.images.approveChosen' }, { n: chosen.length })}
            </Button>
          )}
        </div>
        {failed.length > 0 && (
          <div role="alert" className="tz-card">
            <ul className={img.failed}>
              {failed.map((f) => (
                <li key={f.imageId}>{intl.formatMessage({ id: 'catalogue.images.notApproved' }, { name: nameOf(f.imageId), detail: f.detail })}</li>
              ))}
            </ul>
          </div>
        )}
        {q.isError ? (
          <p role="alert" className="tz-field-error">
            {problemText(q.error)}{' '}
            <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
              {intl.formatMessage({ id: 'common.retry' })}
            </Button>
          </p>
        ) : (
          <DataTable
            label={intl.formatMessage({ id: 'catalogue.images.table' })}
            columns={columns}
            data={rows}
            getRowId={(r) => r.id}
            loading={q.isPending}
            emptyLabel={intl.formatMessage({ id: 'catalogue.images.empty' })}
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
          <ImageCard key={selected.id} image={selected} now={now ?? selected.createdAt} go={go} />
        ) : (
          <p className={`tz-card ${css.muted}`}>
            {intl.formatMessage({ id: search.open && !q.isPending ? 'catalogue.pick.imageDecided' : 'catalogue.pick.image' })}
          </p>
        )}
      </div>
      <ReasonDialog
        open={asking}
        title={intl.formatMessage({ id: 'catalogue.images.bulkTitle' }, { n: chosen.length })}
        description={intl.formatMessage({ id: 'catalogue.images.bulkBody' })}
        confirmLabel={intl.formatMessage({ id: 'catalogue.images.approveChosen' }, { n: chosen.length })}
        label={intl.formatMessage({ id: 'catalogue.review.note' })}
        help={intl.formatMessage({ id: 'catalogue.field.optional' })}
        optional
        pending={bulk.isPending}
        error={bulk.error ? catalogueProblem(bulk.error) : null}
        onConfirm={(reason) =>
          bulk.mutate(
            { ids: chosen, ...(reason ? { reason } : {}) },
            {
              onSuccess: (r) => {
                setAsking(false);
                setPicked(new Set());
                setFailed(r.failed);
                toast.show({ message: intl.formatMessage({ id: 'catalogue.images.bulkDone' }, { n: r.approved.length }) });
              },
            },
          )
        }
        onClose={() => setAsking(false)}
      />
    </div>
  );
}

/** One waiting image beside the product's current one, where it came from, and the two decisions. */
function ImageCard({ image, now, go }: { image: AdminProductImage; now: string; go: Go }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoles();
  const labels = useImageLabels();
  const catalogueProblem = useCatalogueProblem();
  const decide = useDecideImage(image.id, image.productId);
  const [asking, setAsking] = useState<ImageDecision['kind'] | null>(null);
  const may = can(roles, 'catalogueReview');
  const none = intl.formatMessage({ id: 'catalogue.none' });
  const current = imageUrl(image.productImageKey);
  const match = labels.match(image.match);
  const ask = (k: ImageDecision['kind']) => {
    decide.reset();
    setAsking(k);
  };
  const run = (d: ImageDecision, done: string) =>
    decide.mutate(d, {
      onSuccess: () => {
        setAsking(null);
        toast.show({ message: intl.formatMessage({ id: done }) });
      },
    });

  return (
    <>
      <section className="tz-card" aria-labelledby={`image-${image.id}`}>
        <h2 id={`image-${image.id}`} className="tz-card-title">
          {image.productName}
        </h2>
        <div className={img.compare}>
          <figure className={img.figure}>
            <ProductThumb
              large
              name={image.productName}
              src={image.previewUrl ?? undefined}
              alt={intl.formatMessage({ id: 'catalogue.images.alt' }, { name: image.productName })}
            />
            <figcaption>{intl.formatMessage({ id: 'catalogue.images.waiting' })}</figcaption>
          </figure>
          <figure className={img.figure}>
            <ProductThumb
              large
              name={image.productName}
              src={current}
              {...(current ? { alt: intl.formatMessage({ id: 'catalogue.images.altCurrent' }, { name: image.productName }) } : {})}
            />
            <figcaption>{intl.formatMessage({ id: current ? 'catalogue.images.current' : 'catalogue.images.noCurrent' })}</figcaption>
          </figure>
        </div>
        <dl className={css.facts}>
          <dt>{intl.formatMessage({ id: 'catalogue.images.field.product' })}</dt>
          <dd>
            <Button size="sm" variant="ghost" onClick={() => go({ open: image.productId })}>
              {intl.formatMessage({ id: 'catalogue.images.openProduct' })}
            </Button>
          </dd>
          <dt>{intl.formatMessage({ id: 'catalogue.col.brand' })}</dt>
          <dd>{image.brandName ?? none}</dd>
          <dt>{intl.formatMessage({ id: 'catalogue.col.category' })}</dt>
          <dd>{image.categoryName}</dd>
          <dt>{intl.formatMessage({ id: 'catalogue.images.col.source' })}</dt>
          <dd>{labels.source(image.source)}</dd>
          {match && (
            <>
              <dt>{intl.formatMessage({ id: 'catalogue.images.col.match' })}</dt>
              <dd>{match}</dd>
            </>
          )}
          {image.sourceRef && (
            <>
              <dt>{intl.formatMessage({ id: 'catalogue.images.field.barcode' })}</dt>
              <dd className={css.mono}>{image.sourceRef}</dd>
            </>
          )}
          {image.storeName && (
            <>
              <dt>{intl.formatMessage({ id: 'catalogue.col.store' })}</dt>
              <dd>{image.storeName}</dd>
            </>
          )}
          {image.licence && (
            <>
              <dt>{intl.formatMessage({ id: 'catalogue.images.field.licence' })}</dt>
              <dd>{image.licence}</dd>
            </>
          )}
          {(image.attribution ?? image.pageUrl) && (
            <>
              <dt>{intl.formatMessage({ id: 'catalogue.images.field.credit' })}</dt>
              <dd>
                {image.attribution ?? none}
                {image.pageUrl && (
                  <>
                    {' · '}
                    <a href={image.pageUrl} target="_blank" rel="noopener noreferrer">
                      {intl.formatMessage({ id: 'catalogue.images.sourcePage' })}
                    </a>
                  </>
                )}
              </dd>
            </>
          )}
          {image.sizeBytes !== null && (
            <>
              <dt>{intl.formatMessage({ id: 'catalogue.images.field.size' })}</dt>
              <dd className={css.mono}>{intl.formatMessage({ id: 'catalogue.images.size' }, { kb: Math.max(1, Math.round(image.sizeBytes / 1024)) })}</dd>
            </>
          )}
          <dt>{intl.formatMessage({ id: 'catalogue.col.added' })}</dt>
          <dd>{when(image.createdAt, now)}</dd>
        </dl>
        {may && (
          <div className={css.actions}>
            <Button variant="primary" icon={Check} onClick={() => ask('approve')}>
              {intl.formatMessage({ id: 'catalogue.images.approve' })}
            </Button>
            <Button variant="danger" icon={X} onClick={() => ask('reject')}>
              {intl.formatMessage({ id: 'catalogue.images.reject' })}
            </Button>
          </div>
        )}
      </section>
      <AuditPanel type="product" id={image.productId} />
      <ReasonDialog
        open={asking === 'approve'}
        title={intl.formatMessage({ id: 'catalogue.images.approveTitle' }, { name: image.productName })}
        description={intl.formatMessage({ id: 'catalogue.images.approveBody' })}
        confirmLabel={intl.formatMessage({ id: 'catalogue.images.approve' })}
        label={intl.formatMessage({ id: 'catalogue.review.note' })}
        help={intl.formatMessage({ id: 'catalogue.field.optional' })}
        optional
        pending={decide.isPending}
        error={decide.error ? catalogueProblem(decide.error) : null}
        onConfirm={(reason) => run({ kind: 'approve', ...(reason ? { reason } : {}) }, 'catalogue.images.approved')}
        onClose={() => setAsking(null)}
      />
      <ReasonDialog
        open={asking === 'reject'}
        title={intl.formatMessage({ id: 'catalogue.images.rejectTitle' }, { name: image.productName })}
        description={intl.formatMessage({ id: image.source === 'retailer' ? 'catalogue.images.rejectBodyShop' : 'catalogue.images.rejectBody' })}
        confirmLabel={intl.formatMessage({ id: 'catalogue.images.reject' })}
        danger
        pending={decide.isPending}
        error={decide.error ? catalogueProblem(decide.error) : null}
        onConfirm={(reason) => run({ kind: 'reject', reason }, 'catalogue.images.rejected')}
        onClose={() => setAsking(null)}
      />
    </>
  );
}
