import { Button, Skeleton } from '@todayz/ui';
import type { ReactNode } from 'react';
import { useIntl } from 'react-intl';

interface OptionsQuery {
  isPending: boolean;
  isError: boolean;
  isFetching: boolean;
  refetch: () => unknown;
}

/**
 * A choice whose options come from the API (categories, offers, batches). While they load it shows
 * a skeleton the height of the field, never an empty list; if they fail, the reason and a retry.
 */
export function OptionsGate({ query, loadingId, failedId, height = 'var(--control-md)', children }: { query: OptionsQuery; loadingId: string; failedId: string; height?: string; children: ReactNode }) {
  const intl = useIntl();
  if (query.isPending && !query.isError) {
    return (
      <div role="status" aria-busy="true" aria-label={intl.formatMessage({ id: loadingId })}>
        <Skeleton shape="block" height={height} />
      </div>
    );
  }
  if (query.isError) {
    return (
      <div role="alert">
        <p className="tz-field-error">{intl.formatMessage({ id: failedId })}</p>
        <Button size="sm" loading={query.isFetching} onClick={() => void query.refetch()}>
          {intl.formatMessage({ id: 'offers.error.retry' })}
        </Button>
      </div>
    );
  }
  return <>{children}</>;
}
