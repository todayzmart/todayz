import type { AdminCustomer } from '@todayz/api-client/gen/model/adminCustomer';
import type { AdminCustomerOrder } from '@todayz/api-client/gen/model/adminCustomerOrder';
import type { AdminCustomerSession } from '@todayz/api-client/gen/model/adminCustomerSession';
import type { AdminStrike } from '@todayz/api-client/gen/model/adminStrike';
import { ApiError } from '@todayz/api-client';
import { Button, DataTable, Dialog, EmptyState, Skeleton, StatusPill, orderStatusTone, useToast, type ColumnDef, type OrderStatus } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { Ban, RotateCcw, ShieldCheck, UserX } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { money, useWhen } from '../app/format';
import { can, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { ReasonDialog } from '../app/ReasonDialog';
import { AuditPanel } from '../audit/AuditPanel';
import { useOrderLabels } from '../orders/labels';
import { statusTone, useCustomerLabels } from './labels';
import { useCustomer, useCustomerChange, useRevokeSession, type CustomerChange } from './queries';
import css from '../app/page.module.css';

/** A-06 customer detail: strikes, devices, recent orders; block, unblock, strike reset and revoke, each audited. */
export function CustomerPage({ id }: { id: string }) {
  const intl = useIntl();
  const q = useCustomer(id);
  if (q.isPending) return <Skeleton shape="block" height="240px" />;
  if (q.isError) {
    const missing = q.error instanceof ApiError && q.error.code === 'not_found';
    return (
      <EmptyState
        icon={UserX}
        headingLevel={2}
        title={missing ? intl.formatMessage({ id: 'customers.missing' }) : intl.formatMessage({ id: 'customers.loadFailed' })}
        action={
          missing ? (
            <Link to="/customers" className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'customers.back' })}
            </Link>
          ) : (
            <Button onClick={() => void q.refetch()}>{intl.formatMessage({ id: 'common.retry' })}</Button>
          )
        }
      />
    );
  }
  return <Detail c={q.data} />;
}

function Detail({ c }: { c: AdminCustomer }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoles();
  const labels = useCustomerLabels();
  const orderLabels = useOrderLabels();
  const problemText = useProblemText();
  const change = useCustomerChange(c.id);
  const revoke = useRevokeSession(c.id);
  const [open, setOpen] = useState<CustomerChange | null>(null);
  const [revoking, setRevoking] = useState<AdminCustomerSession | null>(null);
  const mayBlock = can(roles, 'customerBlock');
  const mayReset = can(roles, 'strikesReset');
  const now = c.serverNow;

  const confirm = (reason: string) => {
    if (!open) return;
    change.mutate(
      { kind: open, reason },
      {
        onSuccess: () => {
          setOpen(null);
          toast.show({
            message:
              open === 'block'
                ? intl.formatMessage({ id: 'customers.done.block' })
                : open === 'unblock'
                  ? intl.formatMessage({ id: 'customers.done.unblock' })
                  : intl.formatMessage({ id: 'customers.done.reset' }),
          });
        },
      },
    );
  };

  const strikeCols = useMemo<ColumnDef<AdminStrike, unknown>[]>(
    () => [
      { id: 'reason', header: intl.formatMessage({ id: 'customers.col.reason' }), accessorFn: (s) => labels.strike(s.reason) },
      {
        id: 'order',
        header: intl.formatMessage({ id: 'customers.col.order' }),
        cell: ({ row }) =>
          row.original.orderId ? (
            <Link to="/orders/$orderId" params={{ orderId: row.original.orderId }}>
              {intl.formatMessage({ id: 'customers.openOrder' })}
            </Link>
          ) : (
            '—'
          ),
      },
      { id: 'at', header: intl.formatMessage({ id: 'customers.col.when' }), accessorFn: (s) => when(s.createdAt, now) },
      {
        id: 'state',
        header: intl.formatMessage({ id: 'customers.col.state' }),
        cell: ({ row }) =>
          row.original.resetAt ? (
            <span className={css.muted}>
              {intl.formatMessage({ id: 'customers.strike.reset' }, { when: when(row.original.resetAt, now), reason: row.original.resetReason ?? 'none' })}
            </span>
          ) : (
            <StatusPill tone="pending">{intl.formatMessage({ id: 'customers.strike.active' })}</StatusPill>
          ),
      },
    ],
    [intl, labels, when, now],
  );

  const sessionCols = useMemo<ColumnDef<AdminCustomerSession, unknown>[]>(
    () => [
      { id: 'app', header: intl.formatMessage({ id: 'customers.col.app' }), accessorFn: (s) => labels.app(s.app) },
      { id: 'device', header: intl.formatMessage({ id: 'customers.col.device' }), accessorFn: (s) => s.userAgent ?? '—' },
      { id: 'last', header: intl.formatMessage({ id: 'customers.col.lastUsed' }), accessorFn: (s) => when(s.lastUsedAt, now) },
      { id: 'expires', header: intl.formatMessage({ id: 'customers.col.expires' }), accessorFn: (s) => when(s.expiresAt, now) },
      ...(mayBlock
        ? [
            {
              id: 'revoke',
              header: intl.formatMessage({ id: 'customers.col.actions' }),
              cell: ({ row }: { row: { original: AdminCustomerSession } }) => (
                <Button size="sm" variant="soft-danger" onClick={() => setRevoking(row.original)}>
                  {intl.formatMessage({ id: 'customers.revoke' })}
                </Button>
              ),
            } satisfies ColumnDef<AdminCustomerSession, unknown>,
          ]
        : []),
    ],
    [intl, labels, when, now, mayBlock],
  );

  const orderCols = useMemo<ColumnDef<AdminCustomerOrder, unknown>[]>(
    () => [
      {
        id: 'number',
        header: intl.formatMessage({ id: 'customers.col.number' }),
        cell: ({ row }) => (
          <Link to="/orders/$orderId" params={{ orderId: row.original.id }}>
            {row.original.number}
          </Link>
        ),
      },
      { id: 'store', header: intl.formatMessage({ id: 'customers.col.store' }), accessorFn: (o) => o.storeName },
      {
        id: 'status',
        header: intl.formatMessage({ id: 'customers.col.status' }),
        cell: ({ row }) => (
          <StatusPill tone={orderStatusTone(row.original.status as OrderStatus)}>{orderLabels.status(row.original.status)}</StatusPill>
        ),
      },
      { id: 'total', header: intl.formatMessage({ id: 'customers.col.total' }), accessorFn: (o) => money(o.totalPaise), meta: { numeric: true } },
      { id: 'placed', header: intl.formatMessage({ id: 'customers.col.placed' }), accessorFn: (o) => when(o.placedAt, now) },
    ],
    [intl, orderLabels, when, now],
  );

  const name = c.name ?? intl.formatMessage({ id: 'customers.noName' });
  return (
    <div className={css.column}>
      <Link to="/customers" className={css.muted}>
        {intl.formatMessage({ id: 'customers.back' })}
      </Link>
      <div className={css.head}>
        <div>
          <div className={css.title}>
            <h1 className="tz-page-title">{name}</h1>
            <StatusPill tone={statusTone(c.status)}>{labels.status(c.status)}</StatusPill>
          </div>
          <p className="tz-page-sub">
            {intl.formatMessage({ id: 'customers.detail.sub' }, { phone: c.phoneMasked ?? '—', joined: when(c.createdAt, now), strikes: c.activeStrikes })}
          </p>
        </div>
        <div className={css.actions}>
          {mayReset && (
            <Button icon={RotateCcw} disabled={c.activeStrikes === 0} onClick={() => setOpen('reset')}>
              {intl.formatMessage({ id: 'customers.reset' })}
            </Button>
          )}
          {mayBlock && c.status === 'active' && (
            <Button variant="danger" icon={Ban} onClick={() => setOpen('block')}>
              {intl.formatMessage({ id: 'customers.block' })}
            </Button>
          )}
          {mayBlock && c.status === 'blocked' && (
            <Button icon={ShieldCheck} onClick={() => setOpen('unblock')}>
              {intl.formatMessage({ id: 'customers.unblock' })}
            </Button>
          )}
        </div>
      </div>
      <div className={css.detail}>
        <div className={css.column}>
          <section className="tz-card" aria-labelledby="strikes-title">
            <h2 id="strikes-title" className="tz-card-title">
              {intl.formatMessage({ id: 'customers.strikes' }, { count: c.activeStrikes })}
            </h2>
            <DataTable
              label={intl.formatMessage({ id: 'customers.strikes.table' })}
              columns={strikeCols}
              data={c.strikes}
              getRowId={(s) => s.id}
              emptyLabel={intl.formatMessage({ id: 'customers.strikes.none' })}
            />
          </section>
          <section className="tz-card" aria-labelledby="sessions-title">
            <h2 id="sessions-title" className="tz-card-title">
              {intl.formatMessage({ id: 'customers.sessions' })}
            </h2>
            <DataTable
              label={intl.formatMessage({ id: 'customers.sessions' })}
              columns={sessionCols}
              data={c.sessions}
              getRowId={(s) => s.id}
              emptyLabel={intl.formatMessage({ id: 'customers.sessions.none' })}
            />
          </section>
          <section className="tz-card" aria-labelledby="orders-title">
            <h2 id="orders-title" className="tz-card-title">
              {intl.formatMessage({ id: 'customers.orders' })}
            </h2>
            <DataTable
              label={intl.formatMessage({ id: 'customers.orders' })}
              columns={orderCols}
              data={c.recentOrders}
              getRowId={(o) => o.id}
              emptyLabel={intl.formatMessage({ id: 'customers.orders.none' })}
            />
            <Link to="/orders" search={{ customerId: c.id }} className={css.muted}>
              {intl.formatMessage({ id: 'customers.orders.all' })}
            </Link>
          </section>
        </div>
        <AuditPanel type="user" id={c.id} />
      </div>

      <ReasonDialog
        open={open === 'block'}
        title={intl.formatMessage({ id: 'customers.block.title' }, { name })}
        description={intl.formatMessage({ id: 'customers.block.body' })}
        confirmLabel={intl.formatMessage({ id: 'customers.block' })}
        danger
        pending={change.isPending}
        error={change.isError ? problemText(change.error) : null}
        onConfirm={confirm}
        onClose={() => {
          setOpen(null);
          change.reset();
        }}
      />
      <ReasonDialog
        open={open === 'unblock'}
        title={intl.formatMessage({ id: 'customers.unblock.title' }, { name })}
        description={intl.formatMessage({ id: 'customers.unblock.body' })}
        confirmLabel={intl.formatMessage({ id: 'customers.unblock' })}
        pending={change.isPending}
        error={change.isError ? problemText(change.error) : null}
        onConfirm={confirm}
        onClose={() => {
          setOpen(null);
          change.reset();
        }}
      />
      <ReasonDialog
        open={open === 'reset'}
        title={intl.formatMessage({ id: 'customers.reset.title' }, { count: c.activeStrikes })}
        description={intl.formatMessage({ id: 'customers.reset.body' })}
        confirmLabel={intl.formatMessage({ id: 'customers.reset' })}
        pending={change.isPending}
        error={change.isError ? problemText(change.error) : null}
        onConfirm={confirm}
        onClose={() => {
          setOpen(null);
          change.reset();
        }}
      />
      <Dialog
        open={revoking !== null}
        role="alertdialog"
        onClose={() => {
          setRevoking(null);
          revoke.reset();
        }}
        title={intl.formatMessage({ id: 'customers.revoke.title' })}
        description={revoking ? intl.formatMessage({ id: 'customers.revoke.body' }, { app: labels.app(revoking.app) }) : undefined}
        closeLabel={intl.formatMessage({ id: 'common.close' })}
        footer={
          <>
            <Button variant="ghost" onClick={() => setRevoking(null)}>
              {intl.formatMessage({ id: 'common.cancel' })}
            </Button>
            <Button
              variant="danger"
              loading={revoke.isPending}
              loadingLabel={intl.formatMessage({ id: 'common.saving' })}
              onClick={() => {
                if (!revoking) return;
                revoke.mutate(revoking.id, {
                  onSuccess: () => {
                    setRevoking(null);
                    toast.show({ message: intl.formatMessage({ id: 'customers.done.revoke' }) });
                  },
                });
              }}
            >
              {intl.formatMessage({ id: 'customers.revoke' })}
            </Button>
          </>
        }
      >
        {revoke.isError && (
          <p role="alert" className="tz-field-error">
            {problemText(revoke.error)}
          </p>
        )}
      </Dialog>
    </div>
  );
}
