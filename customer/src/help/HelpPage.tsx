import type { MyTicket } from '@todayz/api-client/gen/model/myTicket';
import { Button, EmptyState, Skeleton, StatusPill } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { Inbox, LifeBuoy } from 'lucide-react';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';
import { useFmt } from '../home/format';
import { STATUS_TONE } from './labels';
import { useTickets } from './queries';
import { SignedInOnly } from './SignedInOnly';
import styles from './help.module.css';

/** C-19 Help: the customer's tickets and a way to open one. */
export function HelpPage() {
  const intl = useIntl();
  return (
    <SignedInOnly next="/help">
      <div className={styles.page}>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'help.title' })}</h1>
        <p className={styles.note}>{intl.formatMessage({ id: 'help.intro' })}</p>
        <TicketList />
      </div>
    </SignedInOnly>
  );
}

function TicketList() {
  const intl = useIntl();
  const list = useTickets();
  const items = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  // The list carries no serverNow; "updated 5 min ago" is a label, not a timer, so the device clock will do.
  const now = useMemo(() => new Date(list.dataUpdatedAt || 0).toISOString(), [list.dataUpdatedAt]);

  if (list.isError) {
    return (
      <EmptyState
        icon={Inbox}
        title={intl.formatMessage({ id: 'help.tickets.error.title' })}
        description={intl.formatMessage({ id: 'help.error.body' })}
        action={<Button onClick={() => void list.refetch()}>{intl.formatMessage({ id: 'help.error.retry' })}</Button>}
      />
    );
  }
  if (list.isPending) {
    return (
      <div role="status" aria-label={intl.formatMessage({ id: 'help.tickets.loading' })}>
        <Skeleton height="10rem" />
      </div>
    );
  }
  const newTicket = (
    <Link to="/help/new" className={`tz-btn tz-btn-primary ${styles.linkBtn}`}>
      {intl.formatMessage({ id: 'help.tickets.new' })}
    </Link>
  );
  if (items.length === 0) {
    return (
      <EmptyState
        icon={LifeBuoy}
        title={intl.formatMessage({ id: 'help.tickets.empty.title' })}
        description={intl.formatMessage({ id: 'help.tickets.empty.body' })}
        action={newTicket}
      />
    );
  }
  return (
    <div className={styles.tickets}>
      <div className={styles.actions}>{newTicket}</div>
      <ul className={styles.list} aria-label={intl.formatMessage({ id: 'help.tickets.list' })}>
        {items.map((t) => (
          <TicketRow key={t.id} ticket={t} now={now} />
        ))}
      </ul>
      {list.hasNextPage && (
        <div className={styles.more}>
          <Button loading={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
            {intl.formatMessage({ id: 'help.tickets.more' })}
          </Button>
        </div>
      )}
    </div>
  );
}

function TicketRow({ ticket: t, now }: { ticket: MyTicket; now: string }) {
  const intl = useIntl();
  const { ago } = useFmt();
  return (
    <li>
      <Link to="/help/$ticketId" params={{ ticketId: t.id }} className={`tz-card ${styles.row}`}>
        <span className={styles.rowHead}>
          <span className={styles.number}>{t.number}</span>
          <StatusPill tone={STATUS_TONE[t.status]}>{intl.formatMessage({ id: `help.status.${t.status}` })}</StatusPill>
        </span>
        <span className={styles.subject}>{t.subject}</span>
        <span className={styles.meta}>
          {[
            intl.formatMessage({ id: `help.category.${t.category}` }),
            t.orderNumber,
            intl.formatMessage({ id: 'help.tickets.updated' }, { when: ago(t.lastMessageAt, now) }),
          ]
            .filter(Boolean)
            .join(' · ')}
        </span>
      </Link>
    </li>
  );
}
