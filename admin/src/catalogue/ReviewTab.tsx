import type { AdminPendingProduct } from '@todayz/api-client/gen/model/adminPendingProduct';
import { Button, DataTable, useToast, type ColumnDef } from '@todayz/ui';
import { Check, GitMerge, X } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { can, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { ReasonDialog } from '../app/ReasonDialog';
import { AuditPanel } from '../audit/AuditPanel';
import { AssistSlot } from './AssistSlot';
import type { Go } from './CataloguePage';
import { useCatalogueLabels } from './labels';
import { useCatalogueProblem } from './problem';
import { useDecide, usePending, type Decision } from './queries';
import type { CatalogueSearch } from './search';
import css from '../app/page.module.css';

/** The review queue: products stores added, oldest first, each with the active products it may duplicate. */
export function ReviewTab({ search, go }: { search: CatalogueSearch; go: Go }) {
  const intl = useIntl();
  const when = useWhen();
  const labels = useCatalogueLabels();
  const problemText = useProblemText();
  const q = usePending();
  const pages = q.data?.pages ?? [];
  const rows = pages.flatMap((p) => p.items);
  const now = pages[0]?.serverNow;
  const assist = pages[0]?.assistEnabled ?? false;
  const selected = search.open ? rows.find((r) => r.product.id === search.open) : undefined;

  const columns = useMemo<ColumnDef<AdminPendingProduct, unknown>[]>(
    () => [
      {
        id: 'name',
        header: intl.formatMessage({ id: 'catalogue.col.product' }),
        cell: ({ row }) => (
          <Button
            size="sm"
            variant="ghost"
            aria-pressed={row.original.product.id === search.open}
            onClick={() => go({ tab: 'review', open: row.original.product.id })}
          >
            {labels.name(row.original.product)}
          </Button>
        ),
      },
      {
        id: 'store',
        header: intl.formatMessage({ id: 'catalogue.col.store' }),
        accessorFn: (r) => r.createdByStoreName ?? intl.formatMessage({ id: 'catalogue.none' }),
      },
      { id: 'category', header: intl.formatMessage({ id: 'catalogue.col.category' }), accessorFn: (r) => r.product.categoryName },
      { id: 'pack', header: intl.formatMessage({ id: 'catalogue.col.pack' }), accessorFn: (r) => labels.pack(r.product), meta: { numeric: true } },
      { id: 'matches', header: intl.formatMessage({ id: 'catalogue.col.matches' }), accessorFn: (r) => r.suggestions.length, meta: { numeric: true } },
      { id: 'added', header: intl.formatMessage({ id: 'catalogue.col.added' }), accessorFn: (r) => when(r.product.createdAt, now ?? r.product.createdAt) },
    ],
    [intl, labels, when, now, search.open, go],
  );

  return (
    <div className={css.detail}>
      <div className={css.column}>
        {q.isError ? (
          <p role="alert" className="tz-field-error">
            {problemText(q.error)}{' '}
            <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
              {intl.formatMessage({ id: 'common.retry' })}
            </Button>
          </p>
        ) : (
          <DataTable
            label={intl.formatMessage({ id: 'catalogue.review.table' })}
            columns={columns}
            data={rows}
            getRowId={(r) => r.product.id}
            loading={q.isPending}
            emptyLabel={intl.formatMessage({ id: 'catalogue.review.empty' })}
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
          <PendingCard key={selected.product.id} item={selected} now={now ?? selected.product.createdAt} assist={assist} go={go} />
        ) : search.open && !q.isPending ? (
          <>
            <p className={`tz-card ${css.muted}`}>{intl.formatMessage({ id: 'catalogue.pick.decided' })}</p>
            <AuditPanel type="product" id={search.open} />
          </>
        ) : (
          <p className={`tz-card ${css.muted}`}>{intl.formatMessage({ id: 'catalogue.pick.pending' })}</p>
        )}
      </div>
    </div>
  );
}

type Asking = { kind: 'approve' } | { kind: 'reject' } | { kind: 'merge'; into: string; name: string };

/** One pending product: what the store typed, the likely duplicates, the three decisions, and its history. */
function PendingCard({ item, now, assist, go }: { item: AdminPendingProduct; now: string; assist: boolean; go: Go }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoles();
  const labels = useCatalogueLabels();
  const catalogueProblem = useCatalogueProblem();
  const p = item.product;
  const decide = useDecide(p.id);
  const [asking, setAsking] = useState<Asking | null>(null);
  const may = can(roles, 'catalogueReview');
  const none = intl.formatMessage({ id: 'catalogue.none' });
  const ask = (a: Asking) => {
    decide.reset();
    setAsking(a);
  };
  const run = (d: Decision, done: string) =>
    decide.mutate(d, {
      onSuccess: () => {
        setAsking(null);
        toast.show({ message: intl.formatMessage({ id: done }) });
      },
    });

  return (
    <>
      <section className="tz-card" aria-labelledby={`pending-${p.id}`}>
        <h2 id={`pending-${p.id}`} className="tz-card-title">
          {labels.name(p)}
        </h2>
        <dl className={css.facts}>
          <dt>{intl.formatMessage({ id: 'catalogue.col.store' })}</dt>
          <dd>{item.createdByStoreName ?? none}</dd>
          <dt>{intl.formatMessage({ id: 'catalogue.col.category' })}</dt>
          <dd>{p.categoryName}</dd>
          <dt>{intl.formatMessage({ id: 'catalogue.col.pack' })}</dt>
          <dd className={css.mono}>{labels.pack(p)}</dd>
          <dt>{intl.formatMessage({ id: 'catalogue.col.mrp' })}</dt>
          <dd className={css.mono}>{labels.mrp(p.mrpPaise)}</dd>
          <dt>{intl.formatMessage({ id: 'catalogue.field.barcodes' })}</dt>
          <dd className={css.mono}>{p.barcodes.length ? p.barcodes.join(', ') : none}</dd>
          <dt>{intl.formatMessage({ id: 'catalogue.col.added' })}</dt>
          <dd>{when(p.createdAt, now)}</dd>
        </dl>
        {may && (
          <div className={css.actions}>
            <Button variant="primary" icon={Check} onClick={() => ask({ kind: 'approve' })}>
              {intl.formatMessage({ id: 'catalogue.review.approve' })}
            </Button>
            <Button variant="danger" icon={X} onClick={() => ask({ kind: 'reject' })}>
              {intl.formatMessage({ id: 'catalogue.review.reject' })}
            </Button>
          </div>
        )}
      </section>
      <section className="tz-card" aria-labelledby={`matches-${p.id}`}>
        <h2 id={`matches-${p.id}`} className="tz-card-title">
          {intl.formatMessage({ id: 'catalogue.review.matches' })}
        </h2>
        {item.suggestions.length === 0 ? (
          <p className={css.muted}>{intl.formatMessage({ id: 'catalogue.review.noMatches' })}</p>
        ) : (
          <ul className={css.stack} aria-label={intl.formatMessage({ id: 'catalogue.review.matches' })}>
            {item.suggestions.map((s) => (
              <li key={s.product.id}>
                <Button size="sm" variant="ghost" onClick={() => go({ open: s.product.id })}>
                  {labels.name(s.product)}
                </Button>
                <p className={css.muted}>
                  {intl.formatMessage(
                    { id: 'catalogue.review.match' },
                    { pack: labels.pack(s.product), similarity: labels.similarity(s.similarity), stores: s.product.storeCount },
                  )}
                </p>
                {may && (
                  <Button size="sm" variant="soft" icon={GitMerge} onClick={() => ask({ kind: 'merge', into: s.product.id, name: labels.name(s.product) })}>
                    {intl.formatMessage({ id: 'catalogue.review.mergeInto' })}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>
      {assist && <AssistSlot pending={item} />}
      <AuditPanel type="product" id={p.id} />
      <ReasonDialog
        open={asking?.kind === 'approve'}
        title={intl.formatMessage({ id: 'catalogue.review.approveTitle' }, { name: labels.name(p) })}
        description={intl.formatMessage({ id: 'catalogue.review.approveBody' })}
        confirmLabel={intl.formatMessage({ id: 'catalogue.review.approve' })}
        label={intl.formatMessage({ id: 'catalogue.review.note' })}
        help={intl.formatMessage({ id: 'catalogue.field.optional' })}
        optional
        pending={decide.isPending}
        error={decide.error ? catalogueProblem(decide.error) : null}
        onConfirm={(reason) => run({ kind: 'approve', ...(reason ? { reason } : {}) }, 'catalogue.review.approved')}
        onClose={() => setAsking(null)}
      />
      <ReasonDialog
        open={asking?.kind === 'merge'}
        title={intl.formatMessage({ id: 'catalogue.review.mergeTitle' }, { name: labels.name(p), into: asking?.kind === 'merge' ? asking.name : '' })}
        description={intl.formatMessage({ id: 'catalogue.review.mergeBody' })}
        confirmLabel={intl.formatMessage({ id: 'catalogue.review.merge' })}
        label={intl.formatMessage({ id: 'catalogue.review.note' })}
        help={intl.formatMessage({ id: 'catalogue.field.optional' })}
        optional
        pending={decide.isPending}
        error={decide.error ? catalogueProblem(decide.error) : null}
        onConfirm={(reason) => asking?.kind === 'merge' && run({ kind: 'merge', into: asking.into, ...(reason ? { reason } : {}) }, 'catalogue.review.merged')}
        onClose={() => setAsking(null)}
      />
      <ReasonDialog
        open={asking?.kind === 'reject'}
        title={intl.formatMessage({ id: 'catalogue.review.rejectTitle' }, { name: labels.name(p) })}
        description={intl.formatMessage({ id: 'catalogue.review.rejectBody' })}
        confirmLabel={intl.formatMessage({ id: 'catalogue.review.reject' })}
        danger
        pending={decide.isPending}
        error={decide.error ? catalogueProblem(decide.error) : null}
        onConfirm={(reason) => run({ kind: 'reject', reason }, 'catalogue.review.rejected')}
        onClose={() => setAsking(null)}
      />
    </>
  );
}
