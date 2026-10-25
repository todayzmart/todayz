// C-26 list `/asks` (also linked from Orders): open asks first, then closed, each with its progress line.
import { useSessionState } from '@todayz/auth';
import { useQuery } from '@tanstack/react-query';
import { Button, EmptyState, Skeleton, StatusPill } from '@todayz/ui';
import { MessageCircleQuestion } from 'lucide-react';
import { useIntl } from 'react-intl';
import { RouterLink } from '../home/RouterLink';
import { asksKey, loadAsks, useMyAskStream } from './api';
import { AsksPending, AsksSignedOut } from './AskPage';
import { progressLine } from './progress';
import styles from './asks.module.css';

export function AsksPage() {
  const { status } = useSessionState();
  useMyAskStream();
  if (status === 'signedOut') return <AsksSignedOut next="/asks" />;
  if (status === 'unknown') return <AsksPending />;
  return <AsksList />;
}

function AsksList() {
  const intl = useIntl();
  const q = useQuery({ queryKey: asksKey, queryFn: loadAsks });
  const asks = q.data ? [...q.data.asks].sort((a, b) => Number(b.status === 'open') - Number(a.status === 'open')) : [];

  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'asks.title' })}</h1>
      {q.isPending && <Skeleton shape="block" height="96px" />}
      {q.isError && (
        <EmptyState
          title={intl.formatMessage({ id: 'asks.error.load' })}
          action={
            <Button variant="soft" onClick={() => void q.refetch()}>
              {intl.formatMessage({ id: 'asks.retry' })}
            </Button>
          }
        />
      )}
      {q.data && asks.length === 0 && (
        <EmptyState
          icon={MessageCircleQuestion}
          title={intl.formatMessage({ id: 'asks.empty.title' })}
          description={intl.formatMessage({ id: 'asks.empty.body' })}
          action={
            <RouterLink href="/search" className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'asks.empty.cta' })}
            </RouterLink>
          }
        />
      )}
      {asks.length > 0 && (
        <ul className={styles.list}>
          {asks.map((a) => (
            <li key={a.askId} className="tz-card">
              <RouterLink href={`/asks/${a.askId}`} className={styles.listLink}>
                <span className={styles.listTop}>
                  <span className={styles.shop}>{a.itemName}</span>
                  <StatusPill tone={a.status === 'open' ? 'pending' : 'neutral'}>
                    {intl.formatMessage({ id: a.status === 'open' ? 'asks.status.open' : 'asks.status.closed' })}
                  </StatusPill>
                </span>
                <span className="tz-meta">{progressLine(intl, a)}</span>
                {a.yesCount > 0 && <span className="tz-meta">{intl.formatMessage({ id: 'asks.list.yes' }, { n: a.yesCount })}</span>}
              </RouterLink>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
