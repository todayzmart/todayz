import type { BizTicket } from '@todayz/api-client/gen/model/bizTicket';
import { Banner, Button, EmptyState, Skeleton, StatusPill, Tabs } from '@todayz/ui';
import { Link, useNavigate } from '@tanstack/react-router';
import { Inbox, LifeBuoy } from 'lucide-react';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';
import { whenText } from '../stock/labels';
import { useCurrentBusiness } from '../stores/queries';
import { Guide } from './Guide';
import { STATUS_TONE } from './labels';
import { canTickets } from './permissions';
import { useTickets } from './queries';
import styles from './help.module.css';

export type HelpTab = 'guide' | 'tickets';

/** B-15 Help: the guide and FAQs for every role, and the business's tickets for owners and managers. */
export function HelpPage({ tab }: { tab: HelpTab }) {
  const intl = useIntl();
  const navigate = useNavigate();
  const current = useCurrentBusiness();
  const setTab = (t: HelpTab) => void navigate({ to: '/help', search: t === 'guide' ? {} : { tab: t }, replace: true });

  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'help.title' })}</h1>
      {current.status === 'loading' && (
        <div role="status" aria-label={intl.formatMessage({ id: 'help.loading' })}>
          <Skeleton height="8rem" />
        </div>
      )}
      {current.status === 'error' && (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'help.error.title' })}
          description={intl.formatMessage({ id: 'help.error.body' })}
          action={<Button onClick={current.retry}>{intl.formatMessage({ id: 'help.error.retry' })}</Button>}
        />
      )}
      {current.status === 'none' && <Guide tickets={false} />}
      {current.status === 'ready' &&
        (canTickets(current.business.role) ? (
          <Tabs<HelpTab>
            label={intl.formatMessage({ id: 'help.tabs.label' })}
            value={tab}
            onChange={setTab}
            tabs={[
              { value: 'guide', label: intl.formatMessage({ id: 'help.tabs.guide' }), panel: <Guide tickets /> },
              { value: 'tickets', label: intl.formatMessage({ id: 'help.tabs.tickets' }), panel: <TicketList bid={current.business.id} /> },
            ]}
          />
        ) : (
          <>
            <Banner tone="info">{intl.formatMessage({ id: 'help.askManager' })}</Banner>
            <Guide tickets={false} />
          </>
        ))}
    </div>
  );
}

function TicketList({ bid }: { bid: string }) {
  const intl = useIntl();
  const list = useTickets(bid);
  const items = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);

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
          <TicketRow key={t.id} ticket={t} />
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

function TicketRow({ ticket: t }: { ticket: BizTicket }) {
  const intl = useIntl();
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
            t.storeName,
            t.orderNumber,
            intl.formatMessage({ id: 'help.tickets.updated' }, { when: whenText(intl, t.lastMessageAt) }),
          ]
            .filter(Boolean)
            .join(' · ')}
        </span>
      </Link>
    </li>
  );
}
