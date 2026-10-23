import { formatClock } from '@todayz/i18n';
import { Button } from '@todayz/ui';
import { useNavigate } from '@tanstack/react-router';
import { Check, ScanBarcode } from 'lucide-react';
import { useIntl } from 'react-intl';
import styles from './overview.module.css';

/** Top bar: sync state and the one primary action, New sale (opens Sell). */
export function Topbar({ syncedAt }: { syncedAt: string }) {
  const intl = useIntl();
  const navigate = useNavigate();
  return (
    <div className={`tz-topbar ${styles.topbar}`}>
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
