import { Banner, Button, Skeleton } from '@todayz/ui';
import { useOnline } from './online';
import { useSession, useSessionState } from './react';

export interface SessionPendingProps {
  /** Spoken while the cookie is being tried. */
  loadingLabel: string;
  offlineTitle: string;
  offlineMessage: string;
  retryLabel: string;
  className?: string | undefined;
}

/**
 * What a gate shows while the session is still `unknown`: a skeleton, and when the network or the
 * API cannot be reached, the offline notice with a "Try again" button (it tries the cookie again).
 * Without this an offline first load would show a skeleton for ever.
 */
export function SessionPending({ loadingLabel, offlineTitle, offlineMessage, retryLabel, className }: SessionPendingProps) {
  const session = useSession();
  const { offline } = useSessionState();
  const online = useOnline();
  const stuck = offline || !online;
  return (
    <div className={className} role="status" aria-label={loadingLabel}>
      {stuck && (
        <>
          <Banner tone="warning" live="status" title={offlineTitle}>
            {offlineMessage}
          </Banner>
          <Button onClick={() => void session.start()}>{retryLabel}</Button>
        </>
      )}
      <Skeleton height="2rem" />
      <Skeleton height="8rem" />
    </div>
  );
}
