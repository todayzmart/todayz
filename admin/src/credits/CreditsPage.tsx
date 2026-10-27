import type { AdminExpiryDay } from '@todayz/api-client/gen/model/adminExpiryDay';
import type { AdminLedgerAccount } from '@todayz/api-client/gen/model/adminLedgerAccount';
import type { AdminLedgerEntry } from '@todayz/api-client/gen/model/adminLedgerEntry';
import type { CreditAdjustmentRequest } from '@todayz/api-client/gen/model/creditAdjustmentRequest';
import { ApiError } from '@todayz/api-client';
import { Button, DataTable, EmptyState, Skeleton, StatusPill, Tabs, TextField, useToast, type ColumnDef } from '@todayz/ui';
import { useNavigate } from '@tanstack/react-router';
import { Check, Coins, Search, SlidersHorizontal, X } from 'lucide-react';
import { useCallback, useMemo, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { money, useWhen } from '../app/format';
import { can, useRoles } from '../app/nav/access';
import { ReasonDialog } from '../app/ReasonDialog';
import { StepUpDialog } from '../app/StepUpDialog';
import { AdjustDialog } from './AdjustDialog';
import { statusTone, useCreditLabels, useCreditProblemText } from './labels';
import { useAccountSearch, useAdjustments, useDecision, useEntries, useExpiry, type Decision } from './queries';
import { STATUSES, statusFilter, type CreditsSearch, type CreditsTab } from './search';
import css from '../app/page.module.css';

const stepUp = (e: unknown) => e instanceof ApiError && e.code === 'step_up_required';

/**
 * A-09 Credits: find a ledger account, read its entries and the coming expiry, adjust a customer's
 * wallet, and decide pending adjustment requests. Adjusting and deciding are finance and super admin
 * (D-64) with a reason and a fresh SMS code (D-264); the admin role reads only. An adjustment
 * above the dual-approval threshold waits for a second person, who can never be the requester (D-05).
 */
export function CreditsPage({ search }: { search: CreditsSearch }) {
  const intl = useIntl();
  const navigate = useNavigate();
  const roles = useRoles();
  const manage = can(roles, 'creditAdjust');
  const tab: CreditsTab = search.tab ?? 'ledger';
  const go = useCallback((next: CreditsSearch) => void navigate({ to: '/credits', search: next }), [navigate]);

  return (
    <div className={css.column}>
      <div>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.credits' })}</h1>
        <p className="tz-page-sub">{intl.formatMessage({ id: 'credits.sub' })}</p>
      </div>
      <Tabs<CreditsTab>
        label={intl.formatMessage({ id: 'credits.tabs' })}
        value={tab}
        onChange={(t) => go(t === 'approvals' ? { tab: 'approvals' } : search.account ? { account: search.account } : {})}
        tabs={[
          {
            value: 'ledger',
            label: intl.formatMessage({ id: 'credits.tab.ledger' }),
            panel: <Ledger account={search.account} manage={manage} onOpen={(account) => go({ account })} />,
          },
          {
            value: 'approvals',
            label: intl.formatMessage({ id: 'credits.tab.approvals' }),
            panel: (
              <Approvals
                status={search.status}
                manage={manage}
                onStatus={(status) => go({ tab: 'approvals', ...(status ? { status } : {}) })}
              />
            ),
          },
        ]}
      />
    </div>
  );
}

function Ledger({ account, manage, onOpen }: { account?: string | undefined; manage: boolean; onOpen: (id: string) => void }) {
  const intl = useIntl();
  const labels = useCreditLabels();
  const problemText = useCreditProblemText();
  const find = useAccountSearch();
  const [q, setQ] = useState('');
  const [invalid, setInvalid] = useState(false);
  /** A customer with no wallet account yet (D-240): shown here, not in the URL, since it has no account id. */
  const [unopened, setUnopened] = useState<AdminLedgerAccount | null>(null);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const clean = q.trim();
    setInvalid(clean.length === 0);
    setUnopened(null);
    if (clean) find.mutate(clean);
  };
  /** After the first adjustment the wallet has an account: search again so the row carries its id. */
  const again = () => {
    setUnopened(null);
    if (find.variables) find.mutate(find.variables);
  };

  const columns = useMemo<ColumnDef<AdminLedgerAccount, unknown>[]>(
    () => [
      {
        id: 'owner',
        header: intl.formatMessage({ id: 'credits.col.owner' }),
        cell: ({ row }) => {
          const a = row.original;
          const id = a.id;
          return id ? (
            <Button
              size="sm"
              variant="ghost"
              aria-pressed={!unopened && account === id}
              onClick={() => {
                setUnopened(null);
                onOpen(id);
              }}
            >
              {a.ownerName ?? labels.account(a.kind)}
            </Button>
          ) : (
            <Button size="sm" variant="ghost" aria-pressed={unopened?.ownerId === a.ownerId} onClick={() => setUnopened(a)}>
              {a.ownerName ?? labels.account(a.kind)}
            </Button>
          );
        },
      },
      { id: 'kind', header: intl.formatMessage({ id: 'credits.col.kind' }), accessorFn: (a) => labels.account(a.kind) },
      { id: 'balance', header: intl.formatMessage({ id: 'credits.col.balance' }), accessorFn: (a) => money(a.balancePaise), meta: { numeric: true } },
    ],
    [intl, labels, account, onOpen, unopened],
  );

  return (
    <div className={css.detail}>
      <div className={css.column}>
        <section className="tz-card" aria-labelledby="credits-find">
          <h2 id="credits-find" className="tz-card-title">
            {intl.formatMessage({ id: 'credits.find' })}
          </h2>
          <form className={css.form} onSubmit={submit} noValidate role="search" aria-label={intl.formatMessage({ id: 'credits.find' })}>
            <TextField
              label={intl.formatMessage({ id: 'credits.find.q' })}
              value={q}
              autoComplete="off"
              maxLength={64}
              onChange={(e) => setQ(e.target.value)}
              help={intl.formatMessage({ id: 'credits.find.help' })}
              error={invalid ? intl.formatMessage({ id: 'credits.find.required' }) : undefined}
            />
            <Button type="submit" variant="primary" icon={Search} loading={find.isPending} loadingLabel={intl.formatMessage({ id: 'credits.find.busy' })}>
              {intl.formatMessage({ id: 'credits.find.submit' })}
            </Button>
          </form>
          {find.isError && (
            <p role="alert" className="tz-field-error">
              {problemText(find.error)}
            </p>
          )}
          {find.isSuccess && (
            <DataTable
              label={intl.formatMessage({ id: 'credits.accounts' })}
              columns={columns}
              data={find.data.items}
              getRowId={(a) => a.id ?? `customer-${a.ownerId ?? ''}`}
              emptyLabel={intl.formatMessage({ id: 'credits.accounts.none' })}
            />
          )}
        </section>
        <Expiry />
      </div>
      {unopened ? (
        <UnopenedPanel key={unopened.ownerId} account={unopened} manage={manage} onDone={again} />
      ) : account ? (
        <AccountPanel key={account} id={account} manage={manage} />
      ) : (
        <aside className="tz-card" aria-label={intl.formatMessage({ id: 'credits.panel' })}>
          <p className={css.muted}>{intl.formatMessage({ id: 'credits.panel.empty' })}</p>
        </aside>
      )}
    </div>
  );
}

/** A customer who has never had a ledger posting: no lines, a zero balance, and Adjust opens the wallet (D-240). */
function UnopenedPanel({ account: a, manage, onDone }: { account: AdminLedgerAccount; manage: boolean; onDone: () => void }) {
  const intl = useIntl();
  const labels = useCreditLabels();
  const [adjusting, setAdjusting] = useState(false);
  const name = a.ownerName ?? labels.account(a.kind);
  const headingId = `customer-${a.ownerId ?? ''}`;
  return (
    <aside className={`tz-card ${css.stack}`} aria-labelledby={headingId}>
      <div className={css.title}>
        <h2 id={headingId} className="tz-card-title">
          {name}
        </h2>
      </div>
      <dl className={css.facts}>
        <dt>{intl.formatMessage({ id: 'credits.col.kind' })}</dt>
        <dd>{labels.account(a.kind)}</dd>
        <dt>{intl.formatMessage({ id: 'credits.col.balance' })}</dt>
        <dd className={css.mono}>{money(a.balancePaise)}</dd>
      </dl>
      <p className={css.muted}>{intl.formatMessage({ id: 'credits.panel.unopened' })}</p>
      {manage && a.kind === 'customer_wallet' && (
        <div className={css.actions}>
          <Button variant="primary" icon={SlidersHorizontal} onClick={() => setAdjusting(true)}>
            {intl.formatMessage({ id: 'credits.adjust' })}
          </Button>
        </div>
      )}
      {adjusting && <AdjustDialog account={a} onDone={onDone} onClose={() => setAdjusting(false)} />}
    </aside>
  );
}

function AccountPanel({ id, manage }: { id: string; manage: boolean }) {
  const intl = useIntl();
  const when = useWhen();
  const labels = useCreditLabels();
  const q = useEntries(id);
  const [adjusting, setAdjusting] = useState(false);
  const pages = q.data?.pages ?? [];
  const first = pages[0];
  const now = first?.serverNow;
  const rows = pages.flatMap((p) => p.items);

  const columns = useMemo<ColumnDef<AdminLedgerEntry, unknown>[]>(
    () => [
      { id: 'when', header: intl.formatMessage({ id: 'credits.col.when' }), accessorFn: (e) => when(e.createdAt, now ?? e.createdAt) },
      { id: 'kind', header: intl.formatMessage({ id: 'credits.col.kind' }), accessorFn: (e) => labels.tx(e.kind) },
      { id: 'memo', header: intl.formatMessage({ id: 'credits.col.memo' }), accessorFn: (e) => e.memo ?? (e.refType ? `${e.refType} ${e.refId ?? ''}`.trim() : '—') },
      { id: 'amount', header: intl.formatMessage({ id: 'credits.col.amount' }), accessorFn: (e) => money(e.amountPaise), meta: { numeric: true } },
    ],
    [intl, when, now, labels],
  );

  if (q.isPending) return <Skeleton shape="block" height="320px" />;
  if (q.isError || !first) {
    return (
      <EmptyState
        icon={Coins}
        headingLevel={2}
        title={intl.formatMessage({ id: 'credits.loadFailed' })}
        action={<Button onClick={() => void q.refetch()}>{intl.formatMessage({ id: 'common.retry' })}</Button>}
      />
    );
  }
  const a = first.account;
  const name = a.ownerName ?? labels.account(a.kind);
  return (
    <aside className={`tz-card ${css.stack}`} aria-labelledby={`account-${a.id}`}>
      <div className={css.title}>
        <h2 id={`account-${a.id}`} className="tz-card-title">
          {name}
        </h2>
      </div>
      <dl className={css.facts}>
        <dt>{intl.formatMessage({ id: 'credits.col.kind' })}</dt>
        <dd>{labels.account(a.kind)}</dd>
        <dt>{intl.formatMessage({ id: 'credits.col.balance' })}</dt>
        <dd className={css.mono}>{money(a.balancePaise)}</dd>
      </dl>
      {manage && a.kind === 'customer_wallet' && (
        <div className={css.actions}>
          <Button variant="primary" icon={SlidersHorizontal} onClick={() => setAdjusting(true)}>
            {intl.formatMessage({ id: 'credits.adjust' })}
          </Button>
        </div>
      )}
      <DataTable
        label={intl.formatMessage({ id: 'credits.entries' })}
        columns={columns}
        data={rows}
        getRowId={(e) => e.id}
        emptyLabel={intl.formatMessage({ id: 'credits.entries.none' })}
        footer={
          q.hasNextPage ? (
            <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
              {intl.formatMessage({ id: 'common.loadMore' })}
            </Button>
          ) : undefined
        }
      />
      {adjusting && <AdjustDialog account={a} onClose={() => setAdjusting(false)} />}
    </aside>
  );
}

function Expiry() {
  const intl = useIntl();
  const problemText = useCreditProblemText();
  const q = useExpiry();
  const columns = useMemo<ColumnDef<AdminExpiryDay, unknown>[]>(
    () => [
      { id: 'day', header: intl.formatMessage({ id: 'credits.col.day' }), accessorFn: (d) => d.day },
      { id: 'amount', header: intl.formatMessage({ id: 'credits.col.amount' }), accessorFn: (d) => money(d.expiringPaise), meta: { numeric: true } },
      { id: 'customers', header: intl.formatMessage({ id: 'credits.col.customers' }), accessorFn: (d) => d.customers, meta: { numeric: true } },
    ],
    [intl],
  );
  return (
    <section className="tz-card" aria-labelledby="credits-expiry">
      <h2 id="credits-expiry" className="tz-card-title">
        {q.data ? intl.formatMessage({ id: 'credits.expiry.total' }, { amount: money(q.data.totalPaise) }) : intl.formatMessage({ id: 'credits.expiry' })}
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
          label={intl.formatMessage({ id: 'credits.expiry' })}
          columns={columns}
          data={q.data?.days ?? []}
          getRowId={(d) => d.day}
          loading={q.isPending}
          emptyLabel={intl.formatMessage({ id: 'credits.expiry.none' })}
        />
      )}
    </section>
  );
}

function Approvals({
  status,
  manage,
  onStatus,
}: {
  status: CreditsSearch['status'];
  manage: boolean;
  onStatus: (s: CreditsSearch['status']) => void;
}) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const labels = useCreditLabels();
  const problemText = useCreditProblemText();
  const q = useAdjustments(statusFilter(status));
  const decide = useDecision();
  /** The request being decided, and whether its reject is the requester withdrawing it. */
  const [open, setOpen] = useState<{ kind: Decision['kind']; r: CreditAdjustmentRequest } | null>(null);
  /** The decision waiting on a step-up code; sent again once the code is right. */
  const [pending, setPending] = useState<Decision | null>(null);
  const pages = q.data?.pages ?? [];
  const rows = pages.flatMap((p) => p.items);
  const now = pages[0]?.serverNow;
  const error = decide.error && !stepUp(decide.error) ? problemText(decide.error) : null;

  const close = () => {
    setOpen(null);
    setPending(null);
    decide.reset();
  };
  const send = (d: Decision, r: CreditAdjustmentRequest) => {
    decide.mutate(d, {
      onSuccess: (res) => {
        close();
        const vars = { amount: money(r.amountPaise), name: r.customerName ?? intl.formatMessage({ id: 'credits.noName' }) };
        toast.show({
          message:
            res.outcome === 'approved'
              ? intl.formatMessage({ id: 'credits.approve.done' }, vars)
              : res.outcome === 'cancelled'
                ? intl.formatMessage({ id: 'credits.withdraw.done' }, vars)
                : intl.formatMessage({ id: 'credits.reject.done' }, vars),
        });
      },
      onError: (e) => {
        if (stepUp(e)) setPending(d);
      },
    });
  };

  const columns = useMemo<ColumnDef<CreditAdjustmentRequest, unknown>[]>(
    () => [
      { id: 'customer', header: intl.formatMessage({ id: 'credits.col.customer' }), accessorFn: (r) => r.customerName ?? intl.formatMessage({ id: 'credits.noName' }) },
      { id: 'amount', header: intl.formatMessage({ id: 'credits.col.amount' }), accessorFn: (r) => money(r.amountPaise), meta: { numeric: true } },
      {
        id: 'reason',
        header: intl.formatMessage({ id: 'credits.col.reason' }),
        accessorFn: (r) => (r.link ? intl.formatMessage({ id: 'credits.reasonWithLink' }, { reason: r.reason, link: r.link }) : r.reason),
      },
      {
        id: 'requested',
        header: intl.formatMessage({ id: 'credits.col.requested' }),
        accessorFn: (r) =>
          intl.formatMessage(
            { id: 'credits.requestedBy' },
            { name: r.requestedByName ?? intl.formatMessage({ id: 'credits.noName' }), when: when(r.requestedAt, now ?? r.requestedAt) },
          ),
      },
      {
        id: 'status',
        header: intl.formatMessage({ id: 'credits.col.status' }),
        cell: ({ row }) => <StatusPill tone={statusTone(row.original.status)}>{labels.status(row.original.status)}</StatusPill>,
      },
      {
        id: 'decision',
        header: intl.formatMessage({ id: 'credits.col.decision' }),
        cell: ({ row }) => {
          const r = row.original;
          if (r.status !== 'pending') {
            return r.decisionNote
              ? intl.formatMessage({ id: 'credits.decidedBy' }, { name: r.approvedByName ?? intl.formatMessage({ id: 'credits.noName' }), note: r.decisionNote })
              : '—';
          }
          if (!manage) return <span className={css.muted}>{intl.formatMessage({ id: 'credits.waiting' })}</span>;
          return (
            <div className={css.actions}>
              <Button size="sm" variant="primary" icon={Check} disabled={r.mine} onClick={() => setOpen({ kind: 'approve', r })}>
                {intl.formatMessage({ id: 'credits.approve' })}
              </Button>
              <Button size="sm" icon={X} onClick={() => setOpen({ kind: 'reject', r })}>
                {intl.formatMessage({ id: r.mine ? 'credits.withdraw' : 'credits.reject' })}
              </Button>
              {r.mine && <span className={css.muted}>{intl.formatMessage({ id: 'credits.needsSecond' })}</span>}
            </div>
          );
        },
      },
    ],
    [intl, when, now, labels, manage],
  );

  const r = open?.r;
  const amount = r ? money(r.amountPaise) : '';
  const name = r?.customerName ?? intl.formatMessage({ id: 'credits.noName' });
  const withdraw = open?.kind === 'reject' && r?.mine === true;

  return (
    <section className="tz-card" aria-labelledby="credits-approvals">
      <div className={css.head}>
        <h2 id="credits-approvals" className="tz-card-title">
          {intl.formatMessage({ id: 'credits.tab.approvals' })}
        </h2>
        <div className="tz-field">
          <label className="tz-field-label" htmlFor="credits-status">
            {intl.formatMessage({ id: 'credits.col.status' })}
          </label>
          <select
            id="credits-status"
            className="tz-input"
            value={status ?? 'pending'}
            onChange={(e) => onStatus(e.target.value === 'pending' ? undefined : (e.target.value as CreditsSearch['status']))}
          >
            {STATUSES.map((s) => (
              <option key={s} value={s}>
                {labels.status(s)}
              </option>
            ))}
            <option value="all">{intl.formatMessage({ id: 'credits.status.all' })}</option>
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
          label={intl.formatMessage({ id: 'credits.requests' })}
          columns={columns}
          data={rows}
          getRowId={(x) => x.id}
          loading={q.isPending}
          emptyLabel={intl.formatMessage({ id: 'credits.requests.none' })}
          footer={
            q.hasNextPage ? (
              <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
                {intl.formatMessage({ id: 'common.loadMore' })}
              </Button>
            ) : undefined
          }
        />
      )}
      <ReasonDialog
        open={open !== null}
        title={
          open?.kind === 'approve'
            ? intl.formatMessage({ id: 'credits.approve.title' }, { amount, name })
            : withdraw
              ? intl.formatMessage({ id: 'credits.withdraw.title' }, { amount, name })
              : intl.formatMessage({ id: 'credits.reject.title' }, { amount, name })
        }
        description={r ? intl.formatMessage({ id: 'credits.decide.body' }, { reason: r.reason }) : ''}
        confirmLabel={intl.formatMessage({ id: open?.kind === 'approve' ? 'credits.approve' : withdraw ? 'credits.withdraw' : 'credits.reject' })}
        danger={open?.kind === 'reject'}
        pending={decide.isPending}
        error={error}
        onConfirm={(note) => {
          if (open) send({ kind: open.kind, id: open.r.id, note }, open.r);
        }}
        onClose={close}
      />
      <StepUpDialog
        open={pending !== null}
        onVerified={() => {
          if (pending && open) send(pending, open.r);
        }}
        onClose={() => {
          setPending(null);
          decide.reset();
        }}
      />
    </section>
  );
}
