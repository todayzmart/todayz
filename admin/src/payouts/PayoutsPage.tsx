import type { AdminPayout } from '@todayz/api-client/gen/model/adminPayout';
import type { AdminPayoutDetail } from '@todayz/api-client/gen/model/adminPayoutDetail';
import type { AdminPayoutLine } from '@todayz/api-client/gen/model/adminPayoutLine';
import type { AdminStatement } from '@todayz/api-client/gen/model/adminStatement';
import type { PayoutStatus } from '@todayz/api-client/gen/model/payoutStatus';
import { ApiError } from '@todayz/api-client';
import { Button, DataTable, EmptyState, Skeleton, StatusPill, useToast, type ColumnDef } from '@todayz/ui';
import { useNavigate } from '@tanstack/react-router';
import { Banknote, Download, PauseCircle, PlayCircle } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { money, useWhen } from '../app/format';
import { can, useRoles } from '../app/nav/access';
import { ReasonDialog } from '../app/ReasonDialog';
import { StepUpDialog } from '../app/StepUpDialog';
import { MarkPaidDialog } from './MarkPaidDialog';
import { statusTone, usePayoutLabels, usePayoutProblemText } from './labels';
import { saveCsv, useExportPayouts, usePayout, usePayoutChange, usePayouts, useServerPeriod, useStatements, type PayoutChange } from './queries';
import { PERIOD, STATUSES, type PayoutsSearch } from './search';
import css from '../app/page.module.css';

const stepUp = (e: unknown) => e instanceof ApiError && e.code === 'step_up_required';

const DONE = { hold: 'payouts.done.hold', unhold: 'payouts.done.unhold', paid: 'payouts.done.paid' } as const;

/**
 * A-11 Payouts: a month's statements by business, the payouts it opened, and one payout's lines,
 * hold causes and masked payee. Hold, unhold, mark paid and export are finance and super admin
 * (D-64), each with a reason and a fresh SMS code (D-264); the admin role reads only.
 */
export function PayoutsPage({ search }: { search: PayoutsSearch }) {
  const intl = useIntl();
  const problemText = usePayoutProblemText();
  const server = useServerPeriod(!search.period);
  const period = search.period ?? server.data;
  if (period) return <Month search={search} period={period} />;
  return (
    <div className={css.column}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.payouts' })}</h1>
      {server.isError ? (
        <p role="alert" className="tz-field-error">
          {problemText(server.error)}{' '}
          <Button size="sm" variant="ghost" onClick={() => void server.refetch()}>
            {intl.formatMessage({ id: 'common.retry' })}
          </Button>
        </p>
      ) : (
        <Skeleton shape="block" height="320px" />
      )}
    </div>
  );
}

function Month({ search, period }: { search: PayoutsSearch; period: string }) {
  const intl = useIntl();
  const toast = useToast();
  const roles = useRoles();
  const navigate = useNavigate();
  const problemText = usePayoutProblemText();
  const manage = can(roles, 'payoutManage');
  const exporter = useExportPayouts();
  const [exportPending, setExportPending] = useState(false);

  const go = useCallback((next: PayoutsSearch) => void navigate({ to: '/payouts', search: next }), [navigate]);

  const runExport = () => {
    exporter.mutate(period, {
      onSuccess: (csv) => {
        setExportPending(false);
        exporter.reset();
        saveCsv(`todayz-payouts-${period}.csv`, csv);
        toast.show({ message: intl.formatMessage({ id: 'payouts.export.done' }, { period }) });
      },
      onError: (e) => {
        if (stepUp(e)) setExportPending(true);
        else toast.show({ message: problemText(e) });
      },
    });
  };

  return (
    <div className={css.column}>
      <div className={css.head}>
        <div>
          <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.payouts' })}</h1>
          <p className="tz-page-sub">{intl.formatMessage({ id: 'payouts.sub' })}</p>
        </div>
        <div className={css.form}>
          <div className="tz-field">
            <label className="tz-field-label" htmlFor="payouts-period">
              {intl.formatMessage({ id: 'payouts.period' })}
            </label>
            <input
              id="payouts-period"
              type="month"
              className="tz-input"
              value={period}
              onChange={(e) => {
                if (PERIOD.test(e.target.value)) go({ period: e.target.value });
              }}
            />
          </div>
          {manage && (
            <Button icon={Download} loading={exporter.isPending} loadingLabel={intl.formatMessage({ id: 'payouts.export.busy' })} onClick={runExport}>
              {intl.formatMessage({ id: 'payouts.export' })}
            </Button>
          )}
        </div>
      </div>
      <div className={css.detail}>
        <div className={css.column}>
          <Statements period={period} selected={search.payout} onOpen={(id) => go({ ...search, period, payout: id })} />
          <Payouts
            period={period}
            status={search.status}
            selected={search.payout}
            onStatus={(status) => go({ period, ...(status ? { status } : {}), ...(search.payout ? { payout: search.payout } : {}) })}
            onOpen={(id) => go({ ...search, period, payout: id })}
          />
        </div>
        {search.payout ? (
          <PayoutPanel key={search.payout} id={search.payout} manage={manage} />
        ) : (
          <aside className="tz-card" aria-label={intl.formatMessage({ id: 'payouts.panel' })}>
            <p className={css.muted}>{intl.formatMessage({ id: 'payouts.panel.empty' })}</p>
          </aside>
        )}
      </div>
      <StepUpDialog
        open={exportPending}
        onVerified={runExport}
        onClose={() => {
          setExportPending(false);
          exporter.reset();
        }}
      />
    </div>
  );
}

function Statements({ period, selected, onOpen }: { period: string; selected?: string | undefined; onOpen: (payout: string) => void }) {
  const intl = useIntl();
  const labels = usePayoutLabels();
  const problemText = usePayoutProblemText();
  const q = useStatements(period);
  const rows = (q.data?.pages ?? []).flatMap((p) => p.items);

  const columns = useMemo<ColumnDef<AdminStatement, unknown>[]>(
    () => [
      {
        id: 'business',
        header: intl.formatMessage({ id: 'payouts.col.business' }),
        cell: ({ row }) => {
          const { payout, businessName } = row.original;
          return payout ? (
            <Button size="sm" variant="ghost" aria-pressed={selected === payout.id} onClick={() => onOpen(payout.id)}>
              {businessName}
            </Button>
          ) : (
            businessName
          );
        },
      },
      { id: 'opening', header: intl.formatMessage({ id: 'payouts.col.opening' }), accessorFn: (s) => money(s.totals.openingPaise), meta: { numeric: true } },
      {
        id: 'in',
        header: intl.formatMessage({ id: 'payouts.col.in' }),
        accessorFn: (s) => money(s.totals.creditsInPaise + s.totals.referralInPaise),
        meta: { numeric: true },
      },
      { id: 'cashback', header: intl.formatMessage({ id: 'payouts.col.cashback' }), accessorFn: (s) => money(s.totals.cashbackOutPaise), meta: { numeric: true } },
      { id: 'closing', header: intl.formatMessage({ id: 'payouts.col.closing' }), accessorFn: (s) => money(s.totals.closingPaise), meta: { numeric: true } },
      { id: 'kyc', header: intl.formatMessage({ id: 'payouts.col.kyc' }), accessorFn: (s) => labels.kyc(s.kycStatus) },
      {
        id: 'flags',
        header: intl.formatMessage({ id: 'payouts.col.flags' }),
        accessorFn: (s) =>
          [
            s.businessStatus === 'suspended' ? intl.formatMessage({ id: 'payouts.flag.suspended' }) : null,
            s.openRiskFlag ? intl.formatMessage({ id: 'payouts.flag.risk' }) : null,
          ]
            .filter(Boolean)
            .join(', ') || intl.formatMessage({ id: 'payouts.none' }),
      },
      {
        id: 'payout',
        header: intl.formatMessage({ id: 'payouts.col.payout' }),
        cell: ({ row }) => {
          const p = row.original.payout;
          if (!p) return <span className={css.muted}>{intl.formatMessage({ id: 'payouts.noPayout' })}</span>;
          if (p.carried) return <StatusPill tone="neutral">{intl.formatMessage({ id: 'payouts.carried' })}</StatusPill>;
          return (
            <StatusPill tone={statusTone(p.status)}>
              {intl.formatMessage({ id: 'payouts.pill' }, { status: labels.status(p.status), amount: money(p.amountPaise) })}
            </StatusPill>
          );
        },
      },
    ],
    [intl, labels, selected, onOpen],
  );

  return (
    <section className="tz-card" aria-labelledby="statements-title">
      <h2 id="statements-title" className="tz-card-title">
        {intl.formatMessage({ id: 'payouts.statements' }, { period })}
      </h2>
      {q.isError ? (
        <p role="alert" className="tz-field-error">
          {problemText(q.error)}{' '}
          <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
            {intl.formatMessage({ id: 'common.retry' })}
          </Button>
        </p>
      ) : (
        <DataTable
          label={intl.formatMessage({ id: 'payouts.statements.table' })}
          columns={columns}
          data={rows}
          getRowId={(s) => s.id}
          loading={q.isPending}
          emptyLabel={intl.formatMessage({ id: 'payouts.statements.none' })}
          footer={
            q.hasNextPage ? (
              <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
                {intl.formatMessage({ id: 'common.loadMore' })}
              </Button>
            ) : undefined
          }
        />
      )}
    </section>
  );
}

function Payouts({
  period,
  status,
  selected,
  onStatus,
  onOpen,
}: {
  period: string;
  status?: PayoutStatus | undefined;
  selected?: string | undefined;
  onStatus: (s: PayoutStatus | undefined) => void;
  onOpen: (payout: string) => void;
}) {
  const intl = useIntl();
  const when = useWhen();
  const labels = usePayoutLabels();
  const problemText = usePayoutProblemText();
  const q = usePayouts(period, status);
  const pages = q.data?.pages ?? [];
  const rows = pages.flatMap((p) => p.items);
  const now = pages[0]?.serverNow;

  const columns = useMemo<ColumnDef<AdminPayout, unknown>[]>(
    () => [
      {
        id: 'business',
        header: intl.formatMessage({ id: 'payouts.col.business' }),
        cell: ({ row }) => (
          <Button size="sm" variant="ghost" aria-pressed={selected === row.original.id} onClick={() => onOpen(row.original.id)}>
            {row.original.businessName}
          </Button>
        ),
      },
      { id: 'amount', header: intl.formatMessage({ id: 'payouts.col.amount' }), accessorFn: (p) => money(p.amountPaise), meta: { numeric: true } },
      {
        id: 'status',
        header: intl.formatMessage({ id: 'payouts.col.status' }),
        cell: ({ row }) => <StatusPill tone={statusTone(row.original.status)}>{labels.status(row.original.status)}</StatusPill>,
      },
      { id: 'hold', header: intl.formatMessage({ id: 'payouts.col.hold' }), accessorFn: (p) => labels.hold(p.holdReason) },
      {
        id: 'paid',
        header: intl.formatMessage({ id: 'payouts.col.paid' }),
        accessorFn: (p) =>
          p.utr && p.paidAt ? intl.formatMessage({ id: 'payouts.paidWith' }, { utr: p.utr, when: when(p.paidAt, now ?? p.paidAt) }) : intl.formatMessage({ id: 'payouts.none' }),
      },
    ],
    [intl, when, now, labels, selected, onOpen],
  );

  return (
    <section className="tz-card" aria-labelledby="payouts-title">
      <div className={css.head}>
        <h2 id="payouts-title" className="tz-card-title">
          {intl.formatMessage({ id: 'payouts.list' })}
        </h2>
        <div className="tz-field">
          <label className="tz-field-label" htmlFor="payouts-status">
            {intl.formatMessage({ id: 'payouts.col.status' })}
          </label>
          <select
            id="payouts-status"
            className="tz-input"
            value={status ?? ''}
            onChange={(e) => onStatus(e.target.value ? (e.target.value as PayoutStatus) : undefined)}
          >
            <option value="">{intl.formatMessage({ id: 'payouts.status.all' })}</option>
            {STATUSES.map((s) => (
              <option key={s} value={s}>
                {labels.status(s)}
              </option>
            ))}
          </select>
        </div>
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
          label={intl.formatMessage({ id: 'payouts.list.table' })}
          columns={columns}
          data={rows}
          getRowId={(p) => p.id}
          loading={q.isPending}
          emptyLabel={intl.formatMessage({ id: 'payouts.list.none' })}
          footer={
            q.hasNextPage ? (
              <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
                {intl.formatMessage({ id: 'common.loadMore' })}
              </Button>
            ) : undefined
          }
        />
      )}
    </section>
  );
}

/** One payout: what it pays per store, why it is held, and where it goes (masked). */
function PayoutPanel({ id, manage }: { id: string; manage: boolean }) {
  const intl = useIntl();
  const q = usePayout(id);
  if (q.isPending) return <Skeleton shape="block" height="320px" />;
  if (q.isError) {
    return (
      <EmptyState
        icon={Banknote}
        headingLevel={2}
        title={intl.formatMessage({ id: 'payouts.loadFailed' })}
        action={<Button onClick={() => void q.refetch()}>{intl.formatMessage({ id: 'common.retry' })}</Button>}
      />
    );
  }
  return <PayoutDetail d={q.data} manage={manage} />;
}

function PayoutDetail({ d, manage }: { d: AdminPayoutDetail; manage: boolean }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const labels = usePayoutLabels();
  const problemText = usePayoutProblemText();
  const p = d.payout;
  const now = d.serverNow;
  const change = usePayoutChange(p.id);
  const [dialog, setDialog] = useState<'hold' | 'unhold' | 'paid' | null>(null);
  /** The change waiting on a step-up code; sent again once the code is right. */
  const [pending, setPending] = useState<PayoutChange | null>(null);
  const none = intl.formatMessage({ id: 'payouts.none' });
  const error = change.error && !stepUp(change.error) ? problemText(change.error) : null;

  const close = () => {
    setDialog(null);
    setPending(null);
    change.reset();
  };
  const send = (c: PayoutChange) => {
    change.mutate(c, {
      onSuccess: () => {
        close();
        toast.show({ message: intl.formatMessage({ id: DONE[c.kind] }, { business: p.businessName }) });
      },
      onError: (e) => {
        if (stepUp(e)) setPending(c);
      },
    });
  };

  const lineColumns = useMemo<ColumnDef<AdminPayoutLine, unknown>[]>(
    () => [
      { id: 'store', header: intl.formatMessage({ id: 'payouts.col.store' }), accessorFn: (l) => l.storeName },
      { id: 'amount', header: intl.formatMessage({ id: 'payouts.col.amount' }), accessorFn: (l) => money(l.amountPaise), meta: { numeric: true } },
    ],
    [intl],
  );

  const causes = [
    { key: 'suspended', open: d.causes.businessSuspended, label: intl.formatMessage({ id: 'payouts.cause.suspended' }) },
    { key: 'kyc', open: !d.causes.kycVerified, label: intl.formatMessage({ id: 'payouts.cause.kyc' }) },
    { key: 'details', open: d.causes.payoutDetailsMissing, label: intl.formatMessage({ id: 'payouts.cause.details' }) },
    { key: 'risk', open: d.causes.openRiskFlag, label: intl.formatMessage({ id: 'payouts.cause.risk' }) },
  ];

  return (
    <aside className={`tz-card ${css.stack}`} aria-labelledby={`payout-${p.id}`}>
      <div className={css.title}>
        <h2 id={`payout-${p.id}`} className="tz-card-title">
          {p.businessName}
        </h2>
        <StatusPill tone={statusTone(p.status)}>{labels.status(p.status)}</StatusPill>
      </div>
      <dl className={css.facts}>
        <dt>{intl.formatMessage({ id: 'payouts.col.amount' })}</dt>
        <dd className={css.mono}>{money(p.amountPaise)}</dd>
        <dt>{intl.formatMessage({ id: 'payouts.period' })}</dt>
        <dd>{p.period ?? none}</dd>
        <dt>{intl.formatMessage({ id: 'payouts.col.hold' })}</dt>
        <dd>{p.holdNote ? intl.formatMessage({ id: 'payouts.holdWithNote' }, { hold: labels.hold(p.holdReason), note: p.holdNote }) : labels.hold(p.holdReason)}</dd>
        <dt>{intl.formatMessage({ id: 'payouts.computed' })}</dt>
        <dd>{when(p.computedAt, now)}</dd>
        <dt>{intl.formatMessage({ id: 'payouts.exported' })}</dt>
        <dd>{p.exportedAt ? when(p.exportedAt, now) : none}</dd>
        <dt>{intl.formatMessage({ id: 'payouts.col.paid' })}</dt>
        <dd>{p.utr && p.paidAt ? intl.formatMessage({ id: 'payouts.paidWith' }, { utr: p.utr, when: when(p.paidAt, now) }) : none}</dd>
      </dl>

      <DataTable
        label={intl.formatMessage({ id: 'payouts.lines' })}
        columns={lineColumns}
        data={d.lines}
        getRowId={(l) => l.storeId}
        emptyLabel={intl.formatMessage({ id: 'payouts.lines.none' })}
      />

      <section aria-labelledby={`causes-${p.id}`}>
        <h3 id={`causes-${p.id}`} className="tz-field-label">
          {intl.formatMessage({ id: 'payouts.causes' })}
        </h3>
        <ul className={css.stack}>
          {causes.map((c) => (
            <li key={c.key} className={css.title}>
              <StatusPill tone={c.open ? 'rejected' : 'completed'}>
                {intl.formatMessage({ id: c.open ? 'payouts.cause.open' : 'payouts.cause.clear' })}
              </StatusPill>
              <span>{c.label}</span>
            </li>
          ))}
        </ul>
      </section>

      {d.payee && (
        <section aria-labelledby={`payee-${p.id}`}>
          <h3 id={`payee-${p.id}`} className="tz-field-label">
            {intl.formatMessage({ id: 'payouts.payee' })}
          </h3>
          <dl className={css.facts}>
            <dt>{intl.formatMessage({ id: 'payouts.col.kyc' })}</dt>
            <dd>{labels.kyc(d.payee.kycStatus)}</dd>
            <dt>{intl.formatMessage({ id: 'payouts.payee.upi' })}</dt>
            <dd className={css.mono}>{d.payee.upiIdLast4 ? intl.formatMessage({ id: 'payouts.masked' }, { last4: d.payee.upiIdLast4 }) : none}</dd>
            <dt>{intl.formatMessage({ id: 'payouts.payee.bank' })}</dt>
            <dd className={css.mono}>{d.payee.bankAccountLast4 ? intl.formatMessage({ id: 'payouts.masked' }, { last4: d.payee.bankAccountLast4 }) : none}</dd>
            <dt>{intl.formatMessage({ id: 'payouts.payee.ifsc' })}</dt>
            <dd className={css.mono}>{d.payee.ifsc ?? none}</dd>
          </dl>
        </section>
      )}

      {manage && (p.status === 'pending' || p.status === 'held') && (
        <div className={css.actions}>
          {p.status === 'pending' && (
            <>
              <Button variant="primary" icon={Banknote} onClick={() => setDialog('paid')}>
                {intl.formatMessage({ id: 'payouts.markPaid' })}
              </Button>
              <Button icon={PauseCircle} onClick={() => setDialog('hold')}>
                {intl.formatMessage({ id: 'payouts.holdAction' })}
              </Button>
            </>
          )}
          {p.status === 'held' && (
            <Button icon={PlayCircle} onClick={() => setDialog('unhold')}>
              {intl.formatMessage({ id: 'payouts.unholdAction' })}
            </Button>
          )}
        </div>
      )}

      <ReasonDialog
        open={dialog === 'hold' || dialog === 'unhold'}
        title={intl.formatMessage({ id: dialog === 'unhold' ? 'payouts.unhold.title' : 'payouts.hold.title' }, { business: p.businessName })}
        description={intl.formatMessage({ id: dialog === 'unhold' ? 'payouts.unhold.body' : 'payouts.hold.body' })}
        confirmLabel={intl.formatMessage({ id: dialog === 'unhold' ? 'payouts.unholdAction' : 'payouts.holdAction' })}
        danger={dialog === 'hold'}
        pending={change.isPending}
        error={error}
        onConfirm={(reason) => send({ kind: dialog === 'unhold' ? 'unhold' : 'hold', reason })}
        onClose={close}
      />
      {dialog === 'paid' && (
        <MarkPaidDialog
          payout={p}
          pending={change.isPending}
          error={error}
          onConfirm={(utr, reason) => send({ kind: 'paid', utr, reason, amountPaise: p.amountPaise })}
          onClose={close}
        />
      )}
      <StepUpDialog
        open={pending !== null}
        onVerified={() => {
          if (pending) send(pending);
        }}
        onClose={() => {
          setPending(null);
          change.reset();
        }}
      />
    </aside>
  );
}
