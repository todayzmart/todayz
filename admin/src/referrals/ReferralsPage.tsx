import type { AdminReferral } from '@todayz/api-client/gen/model/adminReferral';
import type { AdminReferralKind } from '@todayz/api-client/gen/model/adminReferralKind';
import type { AdminReferralStatus } from '@todayz/api-client/gen/model/adminReferralStatus';
import { Button, DataTable, StatusPill, useToast, type ColumnDef } from '@todayz/ui';
import { Link, useNavigate } from '@tanstack/react-router';
import { Ban } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { can, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { AuditPanel } from '../audit/AuditPanel';
import { ReasonDialog } from '../risk/ReasonDialog';
import { useRiskLabels } from '../risk/labels';
import { useReferralLabels } from './labels';
import { useReferrals, useReject, type ReferralFilter } from './queries';
import { KINDS, STATUSES, type ReferralsSearch } from './search';
import css from '../app/page.module.css';

/** The contract bound on AdminReferralRejectRequest.reason. */
const MAX_REASON = 300;

type Go = (next: ReferralsSearch) => void;

/**
 * A-10 Referrals: every referral with its parties, credits and status, filtered in the URL.
 * An admin rejects one only before either side is paid (D-225); the open risk flags link to A-13.
 */
export function ReferralsPage({ search }: { search: ReferralsSearch }) {
  const intl = useIntl();
  const when = useWhen();
  const navigate = useNavigate();
  const labels = useReferralLabels();
  const problemText = useProblemText();
  const go: Go = (next) => void navigate({ to: '/referrals', search: next });
  const filter: ReferralFilter = {
    ...(search.status ? { status: search.status } : {}),
    ...(search.kind ? { kind: search.kind } : {}),
    ...(search.flagged ? { flagged: true } : {}),
  };
  const keep = (over: ReferralsSearch): ReferralsSearch => ({
    ...(search.status ? { status: search.status } : {}),
    ...(search.kind ? { kind: search.kind } : {}),
    ...(search.flagged ? { flagged: true } : {}),
    ...over,
  });
  const q = useReferrals(filter);
  const pages = q.data?.pages ?? [];
  const rows = pages.flatMap((p) => p.items);
  const now = pages[0]?.serverNow;
  const chosen = search.referral ? rows.find((r) => r.id === search.referral) : undefined;

  const columns = useMemo<ColumnDef<AdminReferral, unknown>[]>(
    () => [
      {
        id: 'code',
        header: intl.formatMessage({ id: 'referrals.col.code' }),
        cell: ({ row }) => (
          <Button
            size="sm"
            variant="ghost"
            className={css.mono}
            aria-pressed={row.original.id === search.referral}
            onClick={() => go(keep({ referral: row.original.id }))}
          >
            {row.original.code}
          </Button>
        ),
      },
      { id: 'kind', header: intl.formatMessage({ id: 'referrals.col.kind' }), accessorFn: (r) => labels.kind(r.kind) },
      {
        id: 'parties',
        header: intl.formatMessage({ id: 'referrals.col.parties' }),
        accessorFn: (r) =>
          intl.formatMessage(
            { id: 'referrals.parties' },
            { from: labels.party(r.referrer.type, r.referrer.label), to: labels.party(r.referee.type, r.referee.label) },
          ),
      },
      {
        id: 'credits',
        header: intl.formatMessage({ id: 'referrals.col.credits' }),
        accessorFn: (r) => intl.formatMessage({ id: 'referrals.credits' }, { referrer: r.referrerCredits, referee: r.refereeCredits }),
      },
      {
        id: 'status',
        header: intl.formatMessage({ id: 'referrals.col.status' }),
        cell: ({ row }) => <StatusPill tone={labels.tone(row.original.status)}>{labels.status(row.original.status)}</StatusPill>,
      },
      { id: 'flags', header: intl.formatMessage({ id: 'referrals.col.flags' }), accessorFn: (r) => r.openFlags, meta: { numeric: true } },
      { id: 'created', header: intl.formatMessage({ id: 'referrals.col.created' }), accessorFn: (r) => when(r.createdAt, now ?? r.createdAt) },
    ],
    // keep is rebuilt each render from search, which is listed.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [intl, labels, when, now, search],
  );

  return (
    <div className={css.column}>
      <div>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.referrals' })}</h1>
        <p className="tz-page-sub">{intl.formatMessage({ id: 'referrals.sub' })}</p>
      </div>
      <Filters search={search} go={go} />
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
              label={intl.formatMessage({ id: 'referrals.table' })}
              columns={columns}
              data={rows}
              getRowId={(r) => r.id}
              loading={q.isPending}
              emptyLabel={intl.formatMessage({ id: 'referrals.empty' })}
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
          {chosen ? (
            <ReferralCard key={chosen.id} r={chosen} now={now ?? chosen.createdAt} />
          ) : (
            <p className={`tz-card ${css.muted}`}>
              {intl.formatMessage({ id: search.referral && !q.isPending ? 'referrals.notInList' : 'referrals.pick' })}
            </p>
          )}
        </div>
      </div>
    </div>
  );
}

function Filters({ search, go }: { search: ReferralsSearch; go: Go }) {
  const intl = useIntl();
  const labels = useReferralLabels();
  const set = (over: { [K in keyof ReferralsSearch]?: ReferralsSearch[K] | undefined }) => {
    const next = { ...search, ...over };
    go({
      ...(next.status ? { status: next.status } : {}),
      ...(next.kind ? { kind: next.kind } : {}),
      ...(next.flagged ? { flagged: true } : {}),
    });
  };
  return (
    <form className={css.filters} role="search" aria-label={intl.formatMessage({ id: 'referrals.filters' })} onSubmit={(e) => e.preventDefault()}>
      <div className="tz-field">
        <label className="tz-field-label" htmlFor="referrals-status">
          {intl.formatMessage({ id: 'referrals.col.status' })}
        </label>
        <select
          id="referrals-status"
          className="tz-input"
          value={search.status ?? ''}
          onChange={(e) => set({ status: (e.target.value || undefined) as AdminReferralStatus | undefined })}
        >
          <option value="">{intl.formatMessage({ id: 'referrals.status.all' })}</option>
          {STATUSES.map((s) => (
            <option key={s} value={s}>
              {labels.status(s)}
            </option>
          ))}
        </select>
      </div>
      <div className="tz-field">
        <label className="tz-field-label" htmlFor="referrals-kind">
          {intl.formatMessage({ id: 'referrals.col.kind' })}
        </label>
        <select
          id="referrals-kind"
          className="tz-input"
          value={search.kind ?? ''}
          onChange={(e) => set({ kind: (e.target.value || undefined) as AdminReferralKind | undefined })}
        >
          <option value="">{intl.formatMessage({ id: 'referrals.kind.all' })}</option>
          {KINDS.map((k) => (
            <option key={k} value={k}>
              {labels.kind(k)}
            </option>
          ))}
        </select>
      </div>
      <label className={css.title}>
        <input type="checkbox" checked={!!search.flagged} onChange={(e) => set({ flagged: e.target.checked ? true : undefined })} />
        {intl.formatMessage({ id: 'referrals.flagged' })}
      </label>
    </form>
  );
}

/** The chosen referral: both parties, what each gets, its open flags, and the reject. */
function ReferralCard({ r, now }: { r: AdminReferral; now: string }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoles();
  const labels = useReferralLabels();
  const risk = useRiskLabels();
  const problemText = useProblemText();
  const reject = useReject(r.id);
  const [open, setOpen] = useState(false);
  const none = intl.formatMessage({ id: 'referrals.none' });
  // The API also refuses once either side is paid; rewardedAt is the second side's post.
  const rejectable = can(roles, 'referralReject') && (r.status === 'pending' || (r.status === 'qualified' && r.rewardedAt === null));
  return (
    <>
      <section className="tz-card" aria-labelledby={`referral-${r.id}`}>
        <h2 id={`referral-${r.id}`} className="tz-card-title">
          {intl.formatMessage({ id: 'referrals.detail.title' }, { code: r.code })}
        </h2>
        <dl className={css.facts}>
          <dt>{intl.formatMessage({ id: 'referrals.col.status' })}</dt>
          <dd>
            <StatusPill tone={labels.tone(r.status)}>{labels.status(r.status)}</StatusPill>
            {r.rejectReason && <p className={`${css.muted} ${css.mono}`}>{r.rejectReason}</p>}
          </dd>
          <dt>{intl.formatMessage({ id: 'referrals.col.kind' })}</dt>
          <dd>{labels.kind(r.kind)}</dd>
          <dt>{intl.formatMessage({ id: 'referrals.detail.source' })}</dt>
          <dd>{labels.source(r.source)}</dd>
          <dt>{intl.formatMessage({ id: 'referrals.detail.referrer' })}</dt>
          <dd>{intl.formatMessage({ id: 'referrals.detail.party' }, { name: labels.party(r.referrer.type, r.referrer.label), credits: r.referrerCredits })}</dd>
          <dt>{intl.formatMessage({ id: 'referrals.detail.referee' })}</dt>
          <dd>{intl.formatMessage({ id: 'referrals.detail.party' }, { name: labels.party(r.referee.type, r.referee.label), credits: r.refereeCredits })}</dd>
          <dt>{intl.formatMessage({ id: 'referrals.col.created' })}</dt>
          <dd>{when(r.createdAt, now)}</dd>
          <dt>{intl.formatMessage({ id: 'referrals.detail.qualified' })}</dt>
          <dd>{r.qualifiedAt ? when(r.qualifiedAt, now) : none}</dd>
          <dt>{intl.formatMessage({ id: 'referrals.detail.rewarded' })}</dt>
          <dd>{r.rewardedAt ? when(r.rewardedAt, now) : none}</dd>
          <dt>{intl.formatMessage({ id: 'referrals.col.flags' })}</dt>
          <dd>
            {r.flagRules.length === 0 ? (
              none
            ) : (
              <ul className={css.stack}>
                {r.flagRules.map((rule) => (
                  <li key={rule}>
                    <Link to="/risk" search={{ rule }}>
                      {risk.rule(rule)}
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </dd>
        </dl>
        {rejectable && (
          <div className={css.actions}>
            <Button
              variant="danger"
              icon={Ban}
              onClick={() => {
                reject.reset();
                setOpen(true);
              }}
            >
              {intl.formatMessage({ id: 'referrals.reject' })}
            </Button>
          </div>
        )}
      </section>
      <AuditPanel type="referral" id={r.id} />
      <ReasonDialog
        open={open}
        title={intl.formatMessage({ id: 'referrals.rejectTitle' }, { code: r.code })}
        body={intl.formatMessage({ id: 'referrals.rejectBody' })}
        confirmLabel={intl.formatMessage({ id: 'referrals.reject' })}
        max={MAX_REASON}
        danger
        pending={reject.isPending}
        error={reject.error ? problemText(reject.error) : null}
        onClose={() => setOpen(false)}
        onConfirm={(reason) =>
          reject.mutate(reason, {
            onSuccess: () => {
              setOpen(false);
              toast.show({ message: intl.formatMessage({ id: 'referrals.rejectDone' }) });
            },
          })
        }
      />
    </>
  );
}
