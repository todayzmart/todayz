import { SessionPending, useSessionState } from '@todayz/auth';
import { EmptyState } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { LifeBuoy } from 'lucide-react';
import type { ReactNode } from 'react';
import { useIntl } from 'react-intl';
import styles from './help.module.css';

/** Tickets are the signed-in customer's own: a signed-out visitor is sent to sign in and back. */
export function SignedInOnly({ next, children }: { next: string; children: ReactNode }) {
  const intl = useIntl();
  const { status } = useSessionState();
  if (status === 'signedOut') {
    return (
      <div className={styles.page}>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'help.title' })}</h1>
        <EmptyState
          icon={LifeBuoy}
          title={intl.formatMessage({ id: 'help.signedOut.title' })}
          description={intl.formatMessage({ id: 'help.signedOut.body' })}
          action={
            <Link to="/sign-in" search={{ next }} className={`tz-btn tz-btn-primary ${styles.linkBtn}`}>
              {intl.formatMessage({ id: 'help.signedOut.cta' })}
            </Link>
          }
        />
      </div>
    );
  }
  if (status === 'unknown') {
    return (
      <SessionPending
        className={styles.page}
        loadingLabel={intl.formatMessage({ id: 'help.loading' })}
        offlineTitle={intl.formatMessage({ id: 'app.offline.title' })}
        offlineMessage={intl.formatMessage({ id: 'app.offline.body' })}
        retryLabel={intl.formatMessage({ id: 'app.pending.retry' })}
      />
    );
  }
  return <>{children}</>;
}
