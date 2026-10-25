import type { RiskEvidence } from '@todayz/api-client/gen/model/riskEvidence';
import type { RiskFlag } from '@todayz/api-client/gen/model/riskFlag';
import type { RiskFlagDetail } from '@todayz/api-client/gen/model/riskFlagDetail';
import type { RiskFlagStatus } from '@todayz/api-client/gen/model/riskFlagStatus';
import type { RiskResolveRequestFrom } from '@todayz/api-client/gen/model/riskResolveRequestFrom';
import { Button, DataTable, Segmented, Skeleton, StatusPill, Tabs, useToast, type ColumnDef } from '@todayz/ui';
import { useNavigate } from '@tanstack/react-router';
import { Ban, Check, ShieldOff, UserX } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { money, useWhen } from '../app/format';
import { can, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { AuditPanel } from '../audit/AuditPanel';
import { AssistSlot } from './AssistSlot';
import { ReasonDialog } from './ReasonDialog';
import { REWARD_REFUSED, useRiskLabels } from './labels';
import { useFlag, useFlags, useFollowUp, useResolve, type FollowUp, type Verdict } from './queries';
import { STATUSES, type RiskSearch } from './search';
import css from '../app/page.module.css';
import own from './risk.module.css';

/** The contract bounds: RiskReviewNote, and the customer's and business's change reasons. */
const MAX_REVIEW = 500;
const MAX_CHANGE = 500;

type Go = (next: RiskSearch) => void;

/**
 * A-13 Risk: the flags queue by rule, a flag's evidence, clear or confirm with a reason (D-225).
 * Nothing here blocks by itself: after a confirm the page offers the customer's block and the
 * business's suspend, their own audited actions. The one money effect is 01 §9's payout hold:
 * an open or confirmed flag on a business or store holds that business's payout, and only a
 * clear (from open or confirmed) releases it. Support reads only (D-64).
 */
export function RiskPage({ search }: { search: RiskSearch }) {
  const intl = useIntl();
  const when = useWhen();
  const navigate = useNavigate();
  const labels = useRiskLabels();
  const problemText = useProblemText();
  const go: Go = (next) => void navigate({ to: '/risk', search: next });
  const status = search.status ?? 'open';
  const q = useFlags(status, search.rule);
  const pages = q.data?.pages ?? [];
  const rows = pages.flatMap((p) => p.items);
  const now = pages[0]?.serverNow;
  const counts = pages[0]?.openByRule;
  const filters = (over: RiskSearch): RiskSearch => ({
    ...(status !== 'open' ? { status } : {}),
    ...(search.rule ? { rule: search.rule } : {}),
    ...over,
  });

  const tabs = useMemo(() => {
    const open = counts ?? [];
    const t = [{ value: '', label: intl.formatMessage({ id: 'risk.rule.all' }), count: open.reduce((n, c) => n + c.open, 0) }];
    for (const c of open) t.push({ value: c.rule, label: labels.rule(c.rule), count: c.open });
    if (search.rule && !open.some((c) => c.rule === search.rule)) t.push({ value: search.rule, label: labels.rule(search.rule), count: 0 });
    return t;
  }, [counts, intl, labels, search.rule]);

  const columns = useMemo<ColumnDef<RiskFlag, unknown>[]>(
    () => [
      {
        id: 'rule',
        header: intl.formatMessage({ id: 'risk.col.rule' }),
        cell: ({ row }) => (
          <Button
            size="sm"
            variant="ghost"
            aria-pressed={row.original.id === search.flag}
            onClick={() => go(filters({ flag: row.original.id }))}
          >
            {labels.rule(row.original.rule)}
          </Button>
        ),
      },
      {
        id: 'subject',
        header: intl.formatMessage({ id: 'risk.col.subject' }),
        accessorFn: (f) => `${labels.subjectType(f.subjectType)} · ${labels.subject(f.subjectType, f.subjectLabel)}`,
      },
      { id: 'score', header: intl.formatMessage({ id: 'risk.col.score' }), accessorFn: (f) => f.score, meta: { numeric: true } },
      {
        id: 'status',
        header: intl.formatMessage({ id: 'risk.col.status' }),
        cell: ({ row }) => <StatusPill tone={labels.tone(row.original.status)}>{labels.status(row.original.status)}</StatusPill>,
      },
      { id: 'raised', header: intl.formatMessage({ id: 'risk.col.raised' }), accessorFn: (f) => when(f.createdAt, now ?? f.createdAt) },
    ],
    // filters is rebuilt each render from search and status, both listed.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [intl, labels, when, now, search, status],
  );

  return (
    <div className={css.column}>
      <div>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.risk' })}</h1>
        <p className="tz-page-sub">{intl.formatMessage({ id: 'risk.sub' })}</p>
      </div>
      <Segmented<RiskFlagStatus>
        label={intl.formatMessage({ id: 'risk.filter.status' })}
        options={STATUSES.map((s) => ({ value: s, label: labels.status(s) }))}
        value={status}
        onChange={(s) => go({ ...(s !== 'open' ? { status: s } : {}), ...(search.rule ? { rule: search.rule } : {}) })}
      />
      <Tabs
        label={intl.formatMessage({ id: 'risk.tabs' })}
        tabs={tabs}
        value={search.rule ?? ''}
        onChange={(r) => go({ ...(status !== 'open' ? { status } : {}), ...(r ? { rule: r } : {}) })}
      />
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
              label={intl.formatMessage({ id: 'risk.table' })}
              columns={columns}
              data={rows}
              getRowId={(f) => f.id}
              loading={q.isPending}
              emptyLabel={intl.formatMessage({ id: status === 'open' ? 'risk.emptyOpen' : 'risk.empty' })}
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
          {search.flag ? (
            <FlagPanel key={search.flag} id={search.flag} onOpen={(id) => go(filters({ flag: id }))} />
          ) : (
            <p className={`tz-card ${css.muted}`}>{intl.formatMessage({ id: 'risk.pick' })}</p>
          )}
        </div>
      </div>
    </div>
  );
}

function FlagPanel({ id, onOpen }: { id: string; onOpen: (id: string) => void }) {
  const intl = useIntl();
  const problemText = useProblemText();
  const q = useFlag(id);
  if (q.isPending) return <Skeleton shape="block" height="240px" />;
  if (q.isError)
    return (
      <p role="alert" className="tz-card tz-field-error">
        {problemText(q.error)}{' '}
        <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
          {intl.formatMessage({ id: 'common.retry' })}
        </Button>
      </p>
    );
  return <FlagCard d={q.data} onOpen={onOpen} />;
}

/** The chosen flag: what the rule recorded, the rows it counted, the subject, and its other flags. */
function FlagCard({ d, onOpen }: { d: RiskFlagDetail; onOpen: (id: string) => void }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoles();
  const labels = useRiskLabels();
  const problemText = useProblemText();
  const { flag: f, subject: s } = d;
  const resolve = useResolve(f.id);
  const follow = useFollowUp(f.id);
  const [verdict, setVerdict] = useState<Verdict | null>(null);
  const [next, setNext] = useState<FollowUp | null>(null);
  const may = can(roles, 'riskResolve');
  const none = intl.formatMessage({ id: 'risk.none' });
  const rule = labels.rule(f.rule);
  const subject = labels.subject(f.subjectType, f.subjectLabel);
  /** The status a clear or confirm starts from; a cleared flag takes neither. */
  const from: RiskResolveRequestFrom | null = f.status === 'cleared' ? null : f.status;
  const live = from !== null;
  /** What this flag holds while it is open or confirmed (01 §9, D-225): the payout, a referral reward, or nothing. */
  const hold = !live ? 'none' : f.subjectType === 'business' || f.subjectType === 'store' ? 'payout' : f.rule === REWARD_REFUSED ? 'reward' : 'none';
  /** On a referral flag, which side the block or suspend falls on (referee or referrer). */
  const party = next === 'suspend' ? s.businessParty : s.customerParty;
  const blockable = f.status === 'confirmed' && !!s.customerId && can(roles, 'customerBlock') && !(s.type === 'user' && s.status === 'blocked');
  const suspendable = f.status === 'confirmed' && !!s.businessId && can(roles, 'businessVerify') && !(s.type === 'business' && s.status === 'suspended');

  const evidenceCols: ColumnDef<RiskEvidence, unknown>[] = [
    { id: 'kind', header: intl.formatMessage({ id: 'risk.ev.kind' }), accessorFn: (e) => labels.evidence(e.kind) },
    { id: 'label', header: intl.formatMessage({ id: 'risk.ev.what' }), accessorFn: (e) => e.label },
    { id: 'store', header: intl.formatMessage({ id: 'risk.ev.store' }), accessorFn: (e) => e.storeName ?? none },
    { id: 'amount', header: intl.formatMessage({ id: 'risk.ev.amount' }), accessorFn: (e) => (e.amountPaise === null ? none : money(e.amountPaise)), meta: { numeric: true } },
    { id: 'note', header: intl.formatMessage({ id: 'risk.ev.note' }), accessorFn: (e) => e.note ?? none },
    { id: 'at', header: intl.formatMessage({ id: 'risk.ev.at' }), accessorFn: (e) => when(e.at, d.serverNow) },
  ];

  return (
    <>
      <section className="tz-card" aria-labelledby={`flag-${f.id}`}>
        <h2 id={`flag-${f.id}`} className="tz-card-title">
          {intl.formatMessage({ id: 'risk.detail.title' }, { rule, subject })}
        </h2>
        <dl className={css.facts}>
          <dt>{intl.formatMessage({ id: 'risk.col.status' })}</dt>
          <dd>
            <StatusPill tone={labels.tone(f.status)}>{labels.status(f.status)}</StatusPill>
            {hold === 'payout' && <p className={css.muted}>{intl.formatMessage({ id: 'risk.holdsPayout' })}</p>}
            {f.reviewNote && (
              <p className={css.muted}>
                {intl.formatMessage({ id: 'risk.detail.reviewed' }, { who: f.reviewedBy ?? none, reason: f.reviewNote })}
              </p>
            )}
          </dd>
          <dt>{intl.formatMessage({ id: 'risk.col.score' })}</dt>
          <dd className={css.mono}>{f.score}</dd>
          <dt>{intl.formatMessage({ id: 'risk.col.raised' })}</dt>
          <dd>{when(f.createdAt, d.serverNow)}</dd>
          <dt>{intl.formatMessage({ id: 'risk.col.subject' })}</dt>
          <dd>
            {labels.subjectType(s.type)} · {subject}
            {s.status && <p className={css.muted}>{intl.formatMessage({ id: 'risk.detail.subjectStatus' }, { status: s.status })}</p>}
          </dd>
          {d.facts.map((x) => (
            <FactRow key={x.key} k={x.key} v={x.value} />
          ))}
        </dl>
        {may && live && (
          <div className={css.actions}>
            <Button
              icon={ShieldOff}
              onClick={() => {
                resolve.reset();
                setVerdict('clear');
              }}
            >
              {intl.formatMessage({ id: 'risk.clear' })}
            </Button>
            {f.status === 'open' && (
              <Button
                variant="danger"
                icon={Check}
                onClick={() => {
                  resolve.reset();
                  setVerdict('confirm');
                }}
              >
                {intl.formatMessage({ id: 'risk.confirm' })}
              </Button>
            )}
          </div>
        )}
        {(blockable || suspendable) && (
          <div className={css.stack}>
            <p className={css.muted}>{intl.formatMessage({ id: 'risk.next.body' })}</p>
            <div className={css.actions}>
              {blockable && (
                <Button
                  variant="danger"
                  icon={UserX}
                  onClick={() => {
                    follow.reset();
                    setNext('block');
                  }}
                >
                  {intl.formatMessage({ id: 'risk.next.block' })}
                </Button>
              )}
              {suspendable && (
                <Button
                  variant="danger"
                  icon={Ban}
                  onClick={() => {
                    follow.reset();
                    setNext('suspend');
                  }}
                >
                  {intl.formatMessage({ id: 'risk.next.suspend' })}
                </Button>
              )}
            </div>
          </div>
        )}
      </section>
      {d.assistEnabled && <AssistSlot flag={f} />}
      <section className="tz-card" aria-labelledby={`evidence-${f.id}`}>
        <h2 id={`evidence-${f.id}`} className="tz-card-title">
          {intl.formatMessage({ id: 'risk.evidence.title' })}
        </h2>
        <DataTable
          label={intl.formatMessage({ id: 'risk.evidence.title' })}
          columns={evidenceCols}
          data={d.evidence}
          getRowId={(e) => `${e.kind}:${e.id ?? e.at}`}
          emptyLabel={intl.formatMessage({ id: 'risk.evidence.empty' })}
        />
      </section>
      {d.history.length > 0 && (
        <section className="tz-card" aria-labelledby={`others-${f.id}`}>
          <h2 id={`others-${f.id}`} className="tz-card-title">
            {intl.formatMessage({ id: 'risk.others.title' })}
          </h2>
          <ul className={css.stack}>
            {d.history.map((h) => (
              <li key={h.id}>
                <Button size="sm" variant="ghost" onClick={() => onOpen(h.id)}>
                  {labels.rule(h.rule)}
                </Button>{' '}
                <StatusPill tone={labels.tone(h.status)}>{labels.status(h.status)}</StatusPill>{' '}
                <span className={css.muted}>{when(h.createdAt, d.serverNow)}</span>
              </li>
            ))}
          </ul>
        </section>
      )}
      <AuditPanel type="risk_flag" id={f.id} />
      <ReasonDialog
        open={verdict !== null}
        title={intl.formatMessage({ id: verdict === 'confirm' ? 'risk.confirmTitle' : 'risk.clearTitle' }, { rule, subject })}
        body={intl.formatMessage({ id: verdict === 'confirm' ? 'risk.confirmBody' : 'risk.clearBody' }, { hold })}
        confirmLabel={intl.formatMessage({ id: verdict === 'confirm' ? 'risk.confirm' : 'risk.clear' })}
        max={MAX_REVIEW}
        danger={verdict === 'confirm'}
        warning={verdict === 'clear' && f.rule === REWARD_REFUSED ? intl.formatMessage({ id: 'risk.clearRewardWarning' }) : undefined}
        pending={resolve.isPending}
        error={resolve.error ? problemText(resolve.error) : null}
        onClose={() => setVerdict(null)}
        onConfirm={(reason) => {
          if (!verdict || !from) return;
          const done = intl.formatMessage({ id: verdict === 'confirm' ? 'risk.confirmDone' : 'risk.clearDone' });
          resolve.mutate(
            { verdict, from, reason },
            {
              onSuccess: () => {
                setVerdict(null);
                toast.show({ message: done });
              },
            },
          );
        }}
      />
      <ReasonDialog
        open={next !== null}
        title={intl.formatMessage({ id: next === 'suspend' ? 'risk.next.suspendTitle' : 'risk.next.blockTitle' }, { party: party ?? 'none' })}
        body={intl.formatMessage({ id: next === 'suspend' ? 'risk.next.suspendBody' : 'risk.next.blockBody' }, { party: party ?? 'none' })}
        confirmLabel={intl.formatMessage({ id: next === 'suspend' ? 'risk.next.suspend' : 'risk.next.block' })}
        max={MAX_CHANGE}
        danger
        pending={follow.isPending}
        error={follow.error ? problemText(follow.error) : null}
        onClose={() => setNext(null)}
        onConfirm={(reason) => {
          const target = next === 'suspend' ? s.businessId : s.customerId;
          if (!next || !target) return;
          const done = intl.formatMessage({ id: next === 'suspend' ? 'risk.next.suspendDone' : 'risk.next.blockDone' });
          follow.mutate(
            { kind: next, id: target, reason },
            {
              onSuccess: () => {
                setNext(null);
                toast.show({ message: done });
              },
            },
          );
        }}
      />
    </>
  );
}

/** One number the rule recorded; its key is the rule's own name for it. */
function FactRow({ k, v }: { k: string; v: string }) {
  return (
    <>
      <dt className={css.mono}>{k}</dt>
      <dd className={`${css.mono} ${own.value}`}>{v}</dd>
    </>
  );
}
