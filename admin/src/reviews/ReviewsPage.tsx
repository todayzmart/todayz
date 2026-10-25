import type { AdminReview } from '@todayz/api-client/gen/model/adminReview';
import type { AdminReviewReport } from '@todayz/api-client/gen/model/adminReviewReport';
import type { ListAdminReviewsState } from '@todayz/api-client/gen/model/listAdminReviewsState';
import type { ListAdminReviewReportsStatus } from '@todayz/api-client/gen/model/listAdminReviewReportsStatus';
import { Button, DataTable, Segmented, StatusPill, Tabs, TextField, useToast, type ColumnDef } from '@todayz/ui';
import { useNavigate } from '@tanstack/react-router';
import { EyeOff, RotateCcw } from 'lucide-react';
import { useMemo, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { can, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { AuditPanel } from '../audit/AuditPanel';
import { ReasonDialog } from './ReasonDialog';
import { ResolveDialog } from './ResolveDialog';
import { useReviewLabels } from './labels';
import { useModerate, useReports, useResolveReport, useReviews, type Move } from './queries';
import { REPORT_STATUSES, STATES, type ReviewsSearch } from './search';
import css from '../app/page.module.css';

type Go = (next: ReviewsSearch) => void;

/** A-12 Reviews: the review list (reported first) with hide and restore by reason, and the report queue. */
export function ReviewsPage({ search }: { search: ReviewsSearch }) {
  const intl = useIntl();
  const navigate = useNavigate();
  const go: Go = (next) => void navigate({ to: '/reviews', search: next });
  const tab = search.tab ?? 'reviews';
  return (
    <div className={css.column}>
      <div>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.reviews' })}</h1>
        <p className="tz-page-sub">{intl.formatMessage({ id: 'reviews.sub' })}</p>
      </div>
      <Tabs
        label={intl.formatMessage({ id: 'reviews.tabs' })}
        tabs={[
          { value: 'reviews', label: intl.formatMessage({ id: 'reviews.tab.reviews' }) },
          { value: 'reports', label: intl.formatMessage({ id: 'reviews.tab.reports' }) },
        ]}
        value={tab}
        onChange={(v) => go(v === 'reports' ? { tab: 'reports' } : {})}
      />
      {tab === 'reviews' ? <ReviewsTab search={search} go={go} /> : <ReportsTab search={search} go={go} />}
    </div>
  );
}

function ReviewsTab({ search, go }: { search: ReviewsSearch; go: Go }) {
  const intl = useIntl();
  const when = useWhen();
  const labels = useReviewLabels();
  const problemText = useProblemText();
  const state = search.state ?? 'reported';
  const filter = useMemo(() => ({ state, ...(search.storeId ? { storeId: search.storeId } : {}) }), [state, search.storeId]);
  const q = useReviews(filter);
  const pages = q.data?.pages ?? [];
  const rows = pages.flatMap((p) => p.items);
  const now = pages[0]?.serverNow;
  const selected = search.review ? rows.find((r) => r.id === search.review) : undefined;

  const columns = useMemo<ColumnDef<AdminReview, unknown>[]>(
    () => [
      {
        id: 'review',
        header: intl.formatMessage({ id: 'reviews.col.review' }),
        cell: ({ row }) => (
          <Button
            size="sm"
            variant="ghost"
            aria-pressed={row.original.id === search.review}
            onClick={() => go({ ...search, review: row.original.id })}
          >
            {labels.snippet(row.original.text)}
          </Button>
        ),
      },
      { id: 'store', header: intl.formatMessage({ id: 'reviews.col.store' }), accessorFn: (r) => r.storeName },
      { id: 'stars', header: intl.formatMessage({ id: 'reviews.col.stars' }), accessorFn: (r) => labels.stars(r.stars), meta: { numeric: true } },
      { id: 'reports', header: intl.formatMessage({ id: 'reviews.col.reports' }), accessorFn: (r) => r.openReports, meta: { numeric: true } },
      {
        id: 'state',
        header: intl.formatMessage({ id: 'reviews.col.state' }),
        cell: ({ row }) => <StatusPill tone={labels.tone(row.original)}>{labels.state(row.original)}</StatusPill>,
      },
      { id: 'written', header: intl.formatMessage({ id: 'reviews.col.written' }), accessorFn: (r) => when(r.createdAt, now ?? r.createdAt) },
    ],
    [intl, labels, when, now, search, go],
  );

  return (
    <div className={css.detail}>
      <div className={css.column}>
        <Filters key={`${state}:${search.storeId ?? ""}`} search={search} onApply={go} />
        {q.isError ? (
          <p role="alert" className="tz-field-error">
            {problemText(q.error)}{' '}
            <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
              {intl.formatMessage({ id: 'common.retry' })}
            </Button>
          </p>
        ) : (
          <DataTable
            label={intl.formatMessage({ id: 'reviews.table' })}
            columns={columns}
            data={rows}
            getRowId={(r) => r.id}
            loading={q.isPending}
            emptyLabel={intl.formatMessage({ id: 'reviews.empty' })}
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
          <ReviewCard key={selected.id} r={selected} now={now ?? selected.createdAt} />
        ) : (
          <p className={`tz-card ${css.muted}`}>
            {intl.formatMessage({ id: search.review && !q.isPending ? 'reviews.pick.notListed' : 'reviews.pick' })}
          </p>
        )}
      </div>
    </div>
  );
}

function Filters({ search, onApply }: { search: ReviewsSearch; onApply: Go }) {
  const intl = useIntl();
  const labels = useReviewLabels();
  const [state, setState] = useState<ListAdminReviewsState>(search.state ?? 'reported');
  const [storeId, setStoreId] = useState(search.storeId ?? '');
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const id = storeId.trim().toUpperCase();
    onApply({
      ...(state !== 'reported' ? { state } : {}),
      ...(id ? { storeId: id } : {}),
    });
  };
  return (
    <form className={css.filters} onSubmit={submit} role="search" aria-label={intl.formatMessage({ id: 'reviews.filters' })}>
      <div className="tz-field">
        <label className="tz-field-label" htmlFor="reviews-state">
          {intl.formatMessage({ id: 'reviews.filter.state' })}
        </label>
        <select id="reviews-state" className="tz-input" value={state} onChange={(e) => setState(e.target.value as ListAdminReviewsState)}>
          {STATES.map((s) => (
            <option key={s} value={s}>
              {labels.filterState(s)}
            </option>
          ))}
        </select>
      </div>
      <TextField label={intl.formatMessage({ id: 'reviews.filter.store' })} value={storeId} onChange={(e) => setStoreId(e.target.value)} autoComplete="off" />
      <div className={css.actions}>
        <Button type="submit" variant="primary">
          {intl.formatMessage({ id: 'reviews.filter.apply' })}
        </Button>
        <Button type="button" variant="ghost" onClick={() => onApply({})}>
          {intl.formatMessage({ id: 'reviews.filter.clear' })}
        </Button>
      </div>
    </form>
  );
}

/** The chosen review: what was written, its reply, the moves the caller may make, and its history. */
function ReviewCard({ r, now }: { r: AdminReview; now: string }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoles();
  const labels = useReviewLabels();
  const problemText = useProblemText();
  const moderate = useModerate(r.id);
  const [move, setMove] = useState<Move | null>(null);
  const may = can(roles, 'reviewModerate');
  const none = intl.formatMessage({ id: 'reviews.none' });
  const open = (m: Move) => {
    moderate.reset();
    setMove(m);
  };

  return (
    <>
      <section className="tz-card" aria-labelledby={`review-${r.id}`}>
        <h2 id={`review-${r.id}`} className="tz-card-title">
          {intl.formatMessage({ id: 'reviews.detail.title' }, { store: r.storeName })}
        </h2>
        <dl className={css.facts}>
          <dt>{intl.formatMessage({ id: 'reviews.col.stars' })}</dt>
          <dd>{labels.stars(r.stars)}</dd>
          <dt>{intl.formatMessage({ id: 'reviews.detail.text' })}</dt>
          <dd>{r.text ?? none}</dd>
          <dt>{intl.formatMessage({ id: 'reviews.detail.tags' })}</dt>
          <dd>{r.tags.length ? r.tags.join(', ') : none}</dd>
          <dt>{intl.formatMessage({ id: 'reviews.col.written' })}</dt>
          <dd>{when(r.createdAt, now)}</dd>
          <dt>{intl.formatMessage({ id: 'reviews.detail.from' })}</dt>
          <dd>{intl.formatMessage({ id: r.orderId ? 'reviews.detail.fromOrder' : 'reviews.detail.fromSale' })}</dd>
          <dt>{intl.formatMessage({ id: 'reviews.col.reports' })}</dt>
          <dd className={css.mono}>{r.openReports}</dd>
          <dt>{intl.formatMessage({ id: 'reviews.col.state' })}</dt>
          <dd>
            <StatusPill tone={labels.tone(r)}>{labels.state(r)}</StatusPill>
            {r.hidden && r.hideReason && <p className={css.muted}>{intl.formatMessage({ id: 'reviews.detail.why' }, { reason: r.hideReason })}</p>}
          </dd>
          <dt>{intl.formatMessage({ id: 'reviews.detail.reply' })}</dt>
          <dd>
            {r.reply ? (
              <>
                {r.reply.text}
                {r.reply.hidden && (
                  <p className={css.muted}>
                    {intl.formatMessage({ id: 'reviews.detail.replyHidden' }, { reason: r.reply.hideReason ?? 'none' })}
                  </p>
                )}
              </>
            ) : (
              none
            )}
          </dd>
        </dl>
        {may && (
          <div className={css.actions}>
            {r.hidden ? (
              <Button icon={RotateCcw} onClick={() => open('restore')}>
                {intl.formatMessage({ id: 'reviews.move.restore' })}
              </Button>
            ) : (
              <Button variant="danger" icon={EyeOff} onClick={() => open('hide')}>
                {intl.formatMessage({ id: 'reviews.move.hide' })}
              </Button>
            )}
            {r.reply &&
              (r.reply.hidden ? (
                <Button variant="soft" icon={RotateCcw} onClick={() => open('replyRestore')}>
                  {intl.formatMessage({ id: 'reviews.move.replyRestore' })}
                </Button>
              ) : (
                <Button variant="soft" icon={EyeOff} onClick={() => open('replyHide')}>
                  {intl.formatMessage({ id: 'reviews.move.replyHide' })}
                </Button>
              ))}
          </div>
        )}
      </section>
      <AuditPanel type="review" id={r.id} />
      <ReasonDialog
        open={move !== null}
        title={move ? labels.moveTitle(move, r.storeName) : ''}
        body={move ? labels.moveBody(move) : ''}
        confirmLabel={move ? labels.moveConfirm(move) : ''}
        danger={move === 'hide' || move === 'replyHide'}
        pending={moderate.isPending}
        error={moderate.error ? problemText(moderate.error) : null}
        onClose={() => setMove(null)}
        onConfirm={(reason) => {
          if (!move) return;
          const done = labels.moveDone(move);
          moderate.mutate(
            { move, reason },
            {
              onSuccess: () => {
                setMove(null);
                toast.show({ message: done });
              },
            },
          );
        }}
      />
    </>
  );
}

function ReportsTab({ search, go }: { search: ReviewsSearch; go: Go }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoles();
  const labels = useReviewLabels();
  const problemText = useProblemText();
  const status = search.status ?? 'open';
  const q = useReports(status);
  const resolve = useResolveReport();
  const [target, setTarget] = useState<string | null>(null);
  const may = can(roles, 'reviewModerate');
  const pages = q.data?.pages ?? [];
  const rows = pages.flatMap((p) => p.items);
  const now = pages[0]?.serverNow;

  const columns = useMemo<ColumnDef<AdminReviewReport, unknown>[]>(
    () => [
      { id: 'when', header: intl.formatMessage({ id: 'reviews.col.reported' }), accessorFn: (r) => when(r.createdAt, now ?? r.createdAt) },
      { id: 'from', header: intl.formatMessage({ id: 'reviews.col.reporter' }), accessorFn: (r) => labels.reporter(r.reporterKind) },
      { id: 'reason', header: intl.formatMessage({ id: 'reviews.col.reason' }), accessorFn: (r) => r.reason },
      {
        id: 'status',
        header: intl.formatMessage({ id: 'reviews.col.status' }),
        cell: ({ row }) => (
          <>
            <StatusPill tone={row.original.status === 'open' ? 'pending' : 'completed'}>{labels.reportStatus(row.original)}</StatusPill>
            {row.original.resolutionNote && <p className={css.muted}>{row.original.resolutionNote}</p>}
          </>
        ),
      },
      {
        id: 'actions',
        header: intl.formatMessage({ id: 'reviews.col.actions' }),
        cell: ({ row }) =>
          row.original.status === 'open' ? (
            <div className={css.actions}>
              <Button size="sm" variant="ghost" onClick={() => go({ review: row.original.reviewId })}>
                {intl.formatMessage({ id: 'reviews.report.open' })}
              </Button>
              {may && (
                <Button
                  size="sm"
                  variant="soft"
                  onClick={() => {
                    resolve.reset();
                    setTarget(row.original.id);
                  }}
                >
                  {intl.formatMessage({ id: 'reviews.report.resolve' })}
                </Button>
              )}
            </div>
          ) : null,
      },
    ],
    [intl, labels, when, now, go, may, resolve],
  );

  return (
    <div className={css.column}>
      <Segmented<ListAdminReviewReportsStatus>
        label={intl.formatMessage({ id: 'reviews.filter.reportStatus' })}
        options={REPORT_STATUSES.map((s) => ({ value: s, label: labels.reportFilter(s) }))}
        value={status}
        onChange={(s) => go({ tab: 'reports', ...(s === 'open' ? {} : { status: s }) })}
      />
      {q.isError ? (
        <p role="alert" className="tz-field-error">
          {problemText(q.error)}{' '}
          <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
            {intl.formatMessage({ id: 'common.retry' })}
          </Button>
        </p>
      ) : (
        <DataTable
          label={intl.formatMessage({ id: 'reviews.reports.table' })}
          columns={columns}
          data={rows}
          getRowId={(r) => r.id}
          loading={q.isPending}
          emptyLabel={intl.formatMessage({ id: status === 'open' ? 'reviews.reports.emptyOpen' : 'reviews.reports.empty' })}
          footer={
            q.hasNextPage ? (
              <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
                {intl.formatMessage({ id: 'common.loadMore' })}
              </Button>
            ) : undefined
          }
        />
      )}
      <ResolveDialog
        open={target !== null}
        pending={resolve.isPending}
        error={resolve.error ? problemText(resolve.error) : null}
        onClose={() => setTarget(null)}
        onConfirm={(body) => {
          if (!target) return;
          resolve.mutate(
            { id: target, body },
            {
              onSuccess: () => {
                setTarget(null);
                toast.show({ message: intl.formatMessage({ id: 'reviews.resolve.done' }) });
              },
            },
          );
        }}
      />
    </div>
  );
}
