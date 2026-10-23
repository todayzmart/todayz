import { formatClock } from '@todayz/i18n';
import { Button } from '@todayz/ui';
import { useNavigate } from '@tanstack/react-router';
import { Check, ScanBarcode } from 'lucide-react';
import { useIntl } from 'react-intl';
import styles from './overview.module.css';

/**
 * Top bar: sync state and the one primary action, New sale (opens Sell). Below 1024px there is
 * no sidebar, so the bar starts with the TodayZ mark and the store name on a row of its own.
 */
export function Topbar({ syncedAt, storeName }: { syncedAt: string; storeName: string }) {
  const intl = useIntl();
  const navigate = useNavigate();
  return (
    <div className={`tz-topbar ${styles.topbar}`}>
      <span className={styles.brand}>
        <img className={styles.brandMark} src="/icons/mark-96.png" alt={intl.formatMessage({ id: 'shell.appName' })} width={24} height={24} />
        <span className={styles.store}>{storeName}</span>
      </span>
      <span className={styles.spacer} />
      <span className="tz-sync" role="status">
        <Check className="tz-ico tz-ico-sm" aria-hidden="true" />
        {intl.formatMessage({ id: 'overview.synced' }, { time: formatClock(syncedAt) })}
      </span>
      <Button variant="primary" onClick={() => void navigate({ to: '/$section', params: { section: 'sell' } })}>
        <ScanBarcode className="tz-ico tz-ico-sm" aria-hidden="true" />
        {intl.formatMessage({ id: 'overview.newSale' })}
      </Button>
    </div>
  );
}
