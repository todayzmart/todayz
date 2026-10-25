import type { AdminOrder } from '@todayz/api-client/gen/model/adminOrder';
import type { AdminOrderCreditHold } from '@todayz/api-client/gen/model/adminOrderCreditHold';
import type { AdminOrderLine } from '@todayz/api-client/gen/model/adminOrderLine';
import type { AdminOrderNotification } from '@todayz/api-client/gen/model/adminOrderNotification';
import { ApiError } from '@todayz/api-client';
import { Button, DataTable, EmptyState, OrderTimeline, Skeleton, StatusPill, orderStatusTone, useToast, type ColumnDef } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { PackageX, XCircle } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { money, useWhen } from '../app/format';
import { can, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { AuditPanel } from '../audit/AuditPanel';
import { useCustomerLabels } from '../customers/labels';
import { CancelDialog } from './CancelDialog';
import { TERMINAL, useOrderLabels } from './labels';
import { useCancelOrder, useOrder } from './queries';
import css from '../app/page.module.css';

/** A-07 order detail: lines, money, timers, the event timeline, holds and notifications; admin cancel. */
export function OrderPage({ id }: { id: string }) {
  const intl = useIntl();
  const q = useOrder(id);
  if (q.isPending) return <Skeleton shape="block" height="240px" />;
  if (q.isError) {
    const missing = q.error instanceof ApiError && q.error.code === 'not_found';
    return (
      <EmptyState
        icon={PackageX}
        headingLevel={2}
        title={missing ? intl.formatMessage({ id: 'orders.missing' }) : intl.formatMessage({ id: 'orders.loadFailed' })}
        action={
          missing ? (
            <Link to="/orders" className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'orders.back' })}
            </Link>
          ) : (
            <Button onClick={() => void q.refetch()}>{intl.formatMessage({ id: 'common.retry' })}</Button>
          )
        }
      />
    );
  }
  return <Detail o={q.data} />;
}

function Detail({ o }: { o: AdminOrder }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoles();
  const labels = useOrderLabels();
  const customerLabels = useCustomerLabels();
  const problemText = useProblemText();
  const cancel = useCancelOrder(o.id);
  const [open, setOpen] = useState(false);
  const now = o.serverNow;
  const mayCancel = can(roles, 'orderCancel') && !TERMINAL.has(o.status);
  const at = (t: string | null) => (t ? when(t, now) : intl.formatMessage({ id: 'orders.none' }));

  const lineColumns = useMemo<ColumnDef<AdminOrderLine, unknown>[]>(
    () => [
      {
        id: 'name',
        header: intl.formatMessage({ id: 'orders.col.item' }),
        accessorFn: (l) => (l.variant ? `${l.name} · ${l.variant}` : l.name),
      },
      { id: 'price', header: intl.formatMessage({ id: 'orders.col.price' }), accessorFn: (l) => money(l.unitPricePaise), meta: { numeric: true } },
      { id: 'asked', header: intl.formatMessage({ id: 'orders.col.asked' }), accessorFn: (l) => l.quantityRequested, meta: { numeric: true } },
      {
        id: 'accepted',
        header: intl.formatMessage({ id: 'orders.col.accepted' }),
        accessorFn: (l) => l.quantityAccepted ?? intl.formatMessage({ id: 'orders.none' }),
        meta: { numeric: true },
      },
      { id: 'total', header: intl.formatMessage({ id: 'orders.col.total' }), accessorFn: (l) => money(l.lineTotalPaise), meta: { numeric: true } },
    ],
    [intl],
  );

  const holdColumns = useMemo<ColumnDef<AdminOrderCreditHold, unknown>[]>(
    () => [
      { id: 'status', header: intl.formatMessage({ id: 'orders.col.status' }), accessorFn: (h) => h.status },
      { id: 'amount', header: intl.formatMessage({ id: 'orders.col.amount' }), accessorFn: (h) => h.amount, meta: { numeric: true } },
      { id: 'captured', header: intl.formatMessage({ id: 'orders.col.captured' }), accessorFn: (h) => h.captured, meta: { numeric: true } },
      { id: 'released', header: intl.formatMessage({ id: 'orders.col.released' }), accessorFn: (h) => h.released, meta: { numeric: true } },
      { id: 'when', header: intl.formatMessage({ id: 'orders.col.when' }), accessorFn: (h) => when(h.createdAt, now) },
    ],
    [intl, when, now],
  );

  const noticeColumns = useMemo<ColumnDef<AdminOrderNotification, unknown>[]>(
    () => [
      { id: 'app', header: intl.formatMessage({ id: 'orders.col.app' }), accessorFn: (n) => customerLabels.app(n.app) },
      { id: 'kind', header: intl.formatMessage({ id: 'orders.col.kind' }), accessorFn: (n) => n.kind },
      { id: 'when', header: intl.formatMessage({ id: 'orders.col.when' }), accessorFn: (n) => when(n.createdAt, now) },
      {
        id: 'read',
        header: intl.formatMessage({ id: 'orders.col.read' }),
        accessorFn: (n) => (n.readAt ? when(n.readAt, now) : intl.formatMessage({ id: 'orders.unread' })),
      },
    ],
    [intl, customerLabels, when, now],
  );

  const steps = o.events.map((e) => ({
    key: e.id,
    label: labels.status(e.toStatus),
    state: 'done' as const,
    detail: intl.formatMessage(
      { id: 'orders.event.detail' },
      { when: when(e.createdAt, now), actor: labels.actor(e.actorKind), reason: e.reason ?? 'none' },
    ),
  }));

  return (
    <div className={css.detail}>
      <div className={css.column}>
        <Link to="/orders">{intl.formatMessage({ id: 'orders.back' })}</Link>
        <div className={css.head}>
          <div>
            <div className={css.title}>
              <h1 className="tz-page-title">{intl.formatMessage({ id: 'orders.detail.title' }, { number: o.number })}</h1>
              <StatusPill tone={orderStatusTone(o.status)}>{labels.status(o.status)}</StatusPill>
            </div>
            <p className="tz-page-sub">
              {intl.formatMessage({ id: 'orders.detail.sub' }, { fulfilment: labels.fulfilment(o.fulfilment), placed: when(o.placedAt, now) })}
            </p>
            {(o.statusReason || o.statusNote) && (
              <p className={css.muted}>
                {intl.formatMessage({ id: 'orders.detail.reason' }, { reason: o.statusReason ?? 'none', note: o.statusNote ?? 'none' })}
              </p>
            )}
          </div>
          {mayCancel && (
            <div className={css.actions}>
              <Button variant="danger" icon={XCircle} onClick={() => setOpen(true)}>
                {intl.formatMessage({ id: 'orders.cancel' })}
              </Button>
            </div>
          )}
        </div>

        <section className="tz-card" aria-labelledby="order-facts">
          <h2 id="order-facts" className="tz-card-title">
            {intl.formatMessage({ id: 'orders.facts' })}
          </h2>
          <dl className={css.facts}>
            <dt>{intl.formatMessage({ id: 'orders.col.store' })}</dt>
            <dd>{o.storeName}</dd>
            <dt>{intl.formatMessage({ id: 'orders.col.customer' })}</dt>
            <dd>
              <Link to="/customers/$customerId" params={{ customerId: o.customerId }}>
                {o.customerName ?? intl.formatMessage({ id: 'orders.noName' })}
              </Link>
            </dd>
            <dt>{intl.formatMessage({ id: 'orders.money.subtotal' })}</dt>
            <dd className={css.mono}>{money(o.subtotalPaise)}</dd>
            <dt>{intl.formatMessage({ id: 'orders.money.discount' })}</dt>
            <dd className={css.mono}>{money(o.discountPaise)}</dd>
            <dt>{intl.formatMessage({ id: 'orders.money.credits' })}</dt>
            <dd className={css.mono}>{o.creditsUsed}</dd>
            <dt>{intl.formatMessage({ id: 'orders.money.delivery' })}</dt>
            <dd className={css.mono}>{money(o.deliveryFeePaise)}</dd>
            <dt>{intl.formatMessage({ id: 'orders.money.total' })}</dt>
            <dd className={css.mono}>{money(o.totalPaise)}</dd>
            <dt>{intl.formatMessage({ id: 'orders.money.cashback' })}</dt>
            <dd className={css.mono}>{o.cashbackCredits}</dd>
            <dt>{intl.formatMessage({ id: 'orders.money.payment' })}</dt>
            <dd>{o.paymentStatus}</dd>
            <dt>{intl.formatMessage({ id: 'orders.code.attempts' })}</dt>
            <dd>
              {o.codeLocked
                ? intl.formatMessage({ id: 'orders.code.locked' }, { count: o.codeAttempts })
                : intl.formatMessage({ id: 'orders.code.count' }, { count: o.codeAttempts })}
            </dd>
            <dt>{intl.formatMessage({ id: 'orders.timer.acceptBy' })}</dt>
            <dd>{at(o.acceptBy)}</dd>
            <dt>{intl.formatMessage({ id: 'orders.timer.holdUntil' })}</dt>
            <dd>{at(o.holdUntil)}</dd>
            <dt>{intl.formatMessage({ id: 'orders.timer.dispatchBy' })}</dt>
            <dd>{at(o.dispatchBy)}</dd>
          </dl>
        </section>

        <section className="tz-card" aria-labelledby="order-items">
          <h2 id="order-items" className="tz-card-title">
            {intl.formatMessage({ id: 'orders.items' })}
          </h2>
          <DataTable
            label={intl.formatMessage({ id: 'orders.items' })}
            columns={lineColumns}
            data={o.items}
            getRowId={(l) => l.id}
            emptyLabel={intl.formatMessage({ id: 'orders.items.none' })}
          />
        </section>

        <section className="tz-card" aria-labelledby="order-timeline">
          <h2 id="order-timeline" className="tz-card-title">
            {intl.formatMessage({ id: 'orders.timeline' })}
          </h2>
          <OrderTimeline
            label={intl.formatMessage({ id: 'orders.timeline' })}
            steps={steps}
            stateLabels={{
              done: intl.formatMessage({ id: 'orders.step.done' }),
              upcoming: intl.formatMessage({ id: 'orders.step.upcoming' }),
              failed: intl.formatMessage({ id: 'orders.step.failed' }),
            }}
          />
        </section>

        <section className="tz-card" aria-labelledby="order-holds">
          <h2 id="order-holds" className="tz-card-title">
            {intl.formatMessage({ id: 'orders.holds' })}
          </h2>
          <DataTable
            label={intl.formatMessage({ id: 'orders.holds' })}
            columns={holdColumns}
            data={o.creditHolds}
            getRowId={(h) => h.id}
            emptyLabel={intl.formatMessage({ id: 'orders.holds.none' })}
          />
        </section>

        <section className="tz-card" aria-labelledby="order-notices">
          <h2 id="order-notices" className="tz-card-title">
            {intl.formatMessage({ id: 'orders.notices' })}
          </h2>
          <DataTable
            label={intl.formatMessage({ id: 'orders.notices' })}
            columns={noticeColumns}
            data={o.notifications}
            getRowId={(n) => n.id}
            emptyLabel={intl.formatMessage({ id: 'orders.notices.none' })}
          />
        </section>
      </div>

      <AuditPanel type="order" id={o.id} />

      <CancelDialog
        open={open}
        number={o.number}
        pending={cancel.isPending}
        error={cancel.error ? problemText(cancel.error) : null}
        onClose={() => {
          setOpen(false);
          cancel.reset();
        }}
        onConfirm={(body) =>
          cancel.mutate(body, {
            onSuccess: () => {
              setOpen(false);
              toast.show({ message: intl.formatMessage({ id: 'orders.cancel.done' }, { number: o.number }) });
            },
          })
        }
      />
    </div>
  );
}
