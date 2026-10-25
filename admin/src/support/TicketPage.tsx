import type { TicketCloseRequestOutcome } from '@todayz/api-client/gen/model/ticketCloseRequestOutcome';
import type { TicketDetail } from '@todayz/api-client/gen/model/ticketDetail';
import type { TicketMessage } from '@todayz/api-client/gen/model/ticketMessage';
import type { TicketPriority } from '@todayz/api-client/gen/model/ticketPriority';
import { ApiError } from '@todayz/api-client';
import { Button, EmptyState, Skeleton, StatusPill, useToast } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { CheckCircle2, LifeBuoy, Lock, Paperclip } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { useMe } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { ReasonDialog } from '../app/ReasonDialog';
import { AssistSlot } from './AssistSlot';
import { Composer } from './Composer';
import { useSupportLabels } from './labels';
import { useAssign, useClose, usePriority, useTicket } from './queries';
import page from '../app/page.module.css';
import css from './support.module.css';

/** A-14 ticket: the thread with internal notes, assign, reply or note with files, resolve or close. */
export function TicketPage({ id }: { id: string }) {
  const intl = useIntl();
  const q = useTicket(id);
  if (q.isPending) return <Skeleton shape="block" height="240px" />;
  if (q.isError) {
    const missing = q.error instanceof ApiError && q.error.code === 'not_found';
    return (
      <EmptyState
        icon={LifeBuoy}
        headingLevel={2}
        title={missing ? intl.formatMessage({ id: 'support.missing' }) : intl.formatMessage({ id: 'support.loadFailed' })}
        action={
          missing ? (
            <Link to="/support" className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'support.back' })}
            </Link>
          ) : (
            <Button onClick={() => void q.refetch()}>{intl.formatMessage({ id: 'common.retry' })}</Button>
          )
        }
      />
    );
  }
  return <Detail d={q.data} />;
}

function Detail({ d }: { d: TicketDetail }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const labels = useSupportLabels();
  const problemText = useProblemText();
  const close = useClose(d.ticket.id);
  const [outcome, setOutcome] = useState<TicketCloseRequestOutcome | null>(null);
  const t = d.ticket;
  const now = d.serverNow;
  const sla = labels.sla(t, now);
  const isClosed = t.status === 'closed';
  const at = (v: string | null) => (v ? when(v, now) : intl.formatMessage({ id: 'support.never' }));

  return (
    <div className={page.detail}>
      <div className={page.column}>
        <Link to="/support">{intl.formatMessage({ id: 'support.back' })}</Link>
        <div className={page.head}>
          <div>
            <div className={page.title}>
              <h1 className="tz-page-title">{intl.formatMessage({ id: 'support.detail.title' }, { number: t.number })}</h1>
              <StatusPill tone={labels.statusTone(t.status)}>{labels.status(t.status)}</StatusPill>
              <StatusPill tone={labels.priorityTone(t.priority)}>{labels.priority(t.priority)}</StatusPill>
              {sla && <StatusPill tone={sla.tone}>{sla.text}</StatusPill>}
            </div>
            <p className="tz-page-sub">{t.subject}</p>
          </div>
          {!isClosed && (
            <div className={page.actions}>
              {t.status !== 'resolved' && (
                <Button icon={CheckCircle2} onClick={() => setOutcome('resolved')}>
                  {intl.formatMessage({ id: 'support.resolve' })}
                </Button>
              )}
              <Button variant="danger" icon={Lock} onClick={() => setOutcome('closed')}>
                {intl.formatMessage({ id: 'support.close' })}
              </Button>
            </div>
          )}
        </div>

        {d.assistEnabled && <AssistSlot detail={d} />}

        <section className="tz-card" aria-labelledby="ticket-thread">
          <h2 id="ticket-thread" className="tz-card-title">
            {intl.formatMessage({ id: 'support.thread' })}
          </h2>
          <ol className={css.thread}>
            {d.messages.map((m) => (
              <Message key={m.id} m={m} now={now} />
            ))}
          </ol>
        </section>

        <Composer key={isClosed ? 'closed' : 'live'} ticketId={t.id} closed={isClosed} />
      </div>

      <div className={page.column}>
        <Assign d={d} />
        <Priority d={d} />
        <section className="tz-card" aria-labelledby="ticket-facts">
          <h2 id="ticket-facts" className="tz-card-title">
            {intl.formatMessage({ id: 'support.facts' })}
          </h2>
          <dl className={page.facts}>
            <dt>{intl.formatMessage({ id: 'support.col.from' })}</dt>
            <dd>
              {t.openerType === 'customer' ? (
                <Link to="/customers/$customerId" params={{ customerId: t.opener.id }}>
                  {labels.name(t.opener.name)}
                </Link>
              ) : (
                labels.name(t.opener.name)
              )}{' '}
              · {labels.opener(t.openerType)}
            </dd>
            {t.businessId && (
              <>
                <dt>{intl.formatMessage({ id: 'support.business' })}</dt>
                <dd>
                  <Link to="/businesses/$businessId" params={{ businessId: t.businessId }}>
                    {t.businessName}
                  </Link>
                </dd>
              </>
            )}
            {t.storeId && (
              <>
                <dt>{intl.formatMessage({ id: 'support.store' })}</dt>
                <dd>
                  <Link to="/stores/$storeId" params={{ storeId: t.storeId }}>
                    {t.storeName}
                  </Link>
                </dd>
              </>
            )}
            {d.order && (
              <>
                <dt>{intl.formatMessage({ id: 'support.order' })}</dt>
                <dd>
                  <Link to="/orders/$orderId" params={{ orderId: d.order.id }}>
                    {d.order.number}
                  </Link>
                </dd>
              </>
            )}
            <dt>{intl.formatMessage({ id: 'support.col.category' })}</dt>
            <dd>{labels.category(t.category)}</dd>
            <dt>{intl.formatMessage({ id: 'support.opened' })}</dt>
            <dd>{when(t.createdAt, now)}</dd>
            <dt>{intl.formatMessage({ id: 'support.dueAt' })}</dt>
            {/* The reply clock stops while the opener has the turn and once the ticket is done (D-226). */}
            <dd>{t.slaDueAt ? at(t.slaDueAt) : intl.formatMessage({ id: t.status === 'waiting' ? 'support.dueAt.waiting' : 'support.dueAt.none' })}</dd>
            <dt>{intl.formatMessage({ id: 'support.firstResponse' })}</dt>
            <dd>{at(d.firstResponseAt)}</dd>
            {d.resolvedAt && (
              <>
                <dt>{intl.formatMessage({ id: 'support.status.resolved' })}</dt>
                <dd>{when(d.resolvedAt, now)}</dd>
              </>
            )}
            {d.closedAt && (
              <>
                <dt>{intl.formatMessage({ id: 'support.status.closed' })}</dt>
                <dd>{when(d.closedAt, now)}</dd>
              </>
            )}
            {d.closeReason && (
              <>
                <dt>{intl.formatMessage({ id: 'support.closeReason' })}</dt>
                <dd>{d.closeReason}</dd>
              </>
            )}
          </dl>
        </section>
      </div>

      <ReasonDialog
        open={outcome !== null}
        title={
          outcome === 'closed'
            ? intl.formatMessage({ id: 'support.close.title' }, { number: t.number })
            : intl.formatMessage({ id: 'support.resolve.title' }, { number: t.number })
        }
        description={
          outcome === 'closed' ? intl.formatMessage({ id: 'support.close.body' }) : intl.formatMessage({ id: 'support.resolve.body' })
        }
        confirmLabel={outcome === 'closed' ? intl.formatMessage({ id: 'support.close' }) : intl.formatMessage({ id: 'support.resolve' })}
        danger={outcome === 'closed'}
        pending={close.isPending}
        error={close.error ? problemText(close.error) : null}
        onClose={() => {
          setOutcome(null);
          close.reset();
        }}
        onConfirm={(reason) => {
          if (!outcome) return;
          close.mutate(
            { outcome, reason },
            {
              onSuccess: () => {
                setOutcome(null);
                toast.show({
                  message:
                    outcome === 'closed'
                      ? intl.formatMessage({ id: 'support.close.done' }, { number: t.number })
                      : intl.formatMessage({ id: 'support.resolve.done' }, { number: t.number }),
                });
              },
            },
          );
        }}
      />
    </div>
  );
}

function Message({ m, now }: { m: TicketMessage; now: string }) {
  const intl = useIntl();
  const when = useWhen();
  const labels = useSupportLabels();
  const note = m.kind === 'note';
  const cls = [css.message, note ? css.note : m.authorType === 'admin' ? css.fromSupport : ''].filter(Boolean).join(' ');
  return (
    <li className={cls} data-kind={m.kind}>
      <div className={css.meta}>
        <strong>{labels.name(m.author.name)}</strong>
        <span>{labels.author(m.authorType)}</span>
        <span>{when(m.createdAt, now)}</span>
        {note && <StatusPill tone="pending">{intl.formatMessage({ id: 'support.noteTag' })}</StatusPill>}
      </div>
      <p className={css.body}>{m.body}</p>
      {m.attachments.length > 0 && (
        <ul className={css.files} aria-label={intl.formatMessage({ id: 'support.files.list' })}>
          {m.attachments.map((a) => (
            <li key={a.id}>
              <Paperclip size={14} aria-hidden="true" />
              {a.url ? (
                <a href={a.url} target="_blank" rel="noopener noreferrer">
                  {a.fileName}
                </a>
              ) : (
                <span>{a.fileName}</span>
              )}
            </li>
          ))}
        </ul>
      )}
    </li>
  );
}

/** Assign to an agent (or to oneself, or nobody); audited, and refused on a closed ticket. */
function Assign({ d }: { d: TicketDetail }) {
  const intl = useIntl();
  const toast = useToast();
  const labels = useSupportLabels();
  const problemText = useProblemText();
  const me = useMe();
  const assign = useAssign(d.ticket.id);
  const current = d.ticket.assignee?.id ?? '';
  const [choice, setChoice] = useState(current);
  const myId = me.data?.id;
  const closed = d.ticket.status === 'closed';
  const run = (to: string) =>
    assign.mutate(to || null, {
      onSuccess: (next) =>
        toast.show({
          message: next.ticket.assignee
            ? intl.formatMessage({ id: 'support.assign.done' }, { name: labels.name(next.ticket.assignee.name) })
            : intl.formatMessage({ id: 'support.assign.cleared' }),
        }),
    });
  return (
    <section className="tz-card" aria-labelledby="ticket-assign">
      <h2 id="ticket-assign" className="tz-card-title">
        {intl.formatMessage({ id: 'support.col.assignee' })}
      </h2>
      <div className={css.assign}>
        <div className="tz-field">
          <label className="tz-field-label" htmlFor="ticket-assignee">
            {intl.formatMessage({ id: 'support.assign.label' })}
          </label>
          <select id="ticket-assignee" className="tz-input" value={choice} disabled={closed} onChange={(e) => setChoice(e.target.value)}>
            <option value="">{intl.formatMessage({ id: 'support.unassigned' })}</option>
            {d.agents.map((a) => (
              <option key={a.id} value={a.id}>
                {labels.name(a.name)}
              </option>
            ))}
          </select>
        </div>
        <Button
          variant="primary"
          disabled={closed || choice === current}
          loading={assign.isPending}
          loadingLabel={intl.formatMessage({ id: 'common.saving' })}
          onClick={() => run(choice)}
        >
          {intl.formatMessage({ id: 'support.assign' })}
        </Button>
        {myId && myId !== current && !closed && d.agents.some((a) => a.id === myId) && (
          <Button
            variant="soft"
            onClick={() => {
              setChoice(myId);
              run(myId);
            }}
          >
            {intl.formatMessage({ id: 'support.assign.me' })}
          </Button>
        )}
      </div>
      {assign.error && (
        <p role="alert" className="tz-field-error">
          {problemText(assign.error)}
        </p>
      )}
    </section>
  );
}

const PRIORITIES: readonly TicketPriority[] = ['normal', 'high', 'urgent'];

/**
 * Change the priority (audited, D-226); refused on a closed ticket. An open ticket's due time moves to
 * its timer's start plus the new priority's minutes, so it can turn overdue at once.
 */
function Priority({ d }: { d: TicketDetail }) {
  const intl = useIntl();
  const toast = useToast();
  const labels = useSupportLabels();
  const problemText = useProblemText();
  const change = usePriority(d.ticket.id);
  const current = d.ticket.priority;
  const [choice, setChoice] = useState<TicketPriority>(current);
  const closed = d.ticket.status === 'closed';
  return (
    <section className="tz-card" aria-labelledby="ticket-priority">
      <h2 id="ticket-priority" className="tz-card-title">
        {intl.formatMessage({ id: 'support.col.priority' })}
      </h2>
      <div className={css.assign}>
        <div className="tz-field">
          <label className="tz-field-label" htmlFor="ticket-priority-pick">
            {intl.formatMessage({ id: 'support.priority.label' })}
          </label>
          <select
            id="ticket-priority-pick"
            className="tz-input"
            value={choice}
            disabled={closed}
            onChange={(e) => setChoice(e.target.value as TicketPriority)}
          >
            {PRIORITIES.map((p) => (
              <option key={p} value={p}>
                {labels.priority(p)}
              </option>
            ))}
          </select>
        </div>
        <Button
          variant="primary"
          disabled={closed || choice === current}
          loading={change.isPending}
          loadingLabel={intl.formatMessage({ id: 'common.saving' })}
          onClick={() =>
            change.mutate(choice, {
              onSuccess: (next) =>
                toast.show({
                  message: intl.formatMessage(
                    { id: 'support.priority.done' },
                    { priority: labels.priority(next.ticket.priority) },
                  ),
                }),
            })
          }
        >
          {intl.formatMessage({ id: 'support.priority.change' })}
        </Button>
      </div>
      {!closed && <p className="tz-field-help">{intl.formatMessage({ id: 'support.priority.help' })}</p>}
      {change.error && (
        <p role="alert" className="tz-field-error">
          {problemText(change.error)}
        </p>
      )}
    </section>
  );
}
