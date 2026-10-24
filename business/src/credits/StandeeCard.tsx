import { Button, useToast } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { Download, Eye } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { getStandeePdf } from './api';
import styles from './credits.module.css';
import { useShortCode } from './queries';

type Mode = 'preview' | 'download';

/**
 * The QR standee: the store short code (the one in the standee URL, D-26) and the A5 PDF. The PDF is
 * fetched with the session token by `request` and shown from a blob URL, so no token is in a URL.
 */
export function StandeeCard({ store }: { store: CurrentStore }) {
  const intl = useIntl();
  const toast = useToast();
  const code = useShortCode(store.bid, store.sid);
  const [busy, setBusy] = useState<Mode | null>(null);

  const run = async (mode: Mode) => {
    setBusy(mode);
    try {
      const blob = await getStandeePdf(store.bid, store.sid);
      const url = URL.createObjectURL(blob);
      if (mode === 'preview') {
        window.open(url, '_blank', 'noopener');
      } else {
        const a = document.createElement('a');
        a.href = url;
        a.download = 'todayz-standee-a5.pdf';
        a.click();
      }
      window.setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch {
      toast.show({ message: intl.formatMessage({ id: 'credits.standee.error' }) });
    } finally {
      setBusy(null);
    }
  };

  return (
    <section className="tz-card" aria-labelledby="cr-standee-title">
      <div className="tz-card-head">
        <h2 className="tz-card-title" id="cr-standee-title">
          {intl.formatMessage({ id: 'credits.standee.title' })}
        </h2>
      </div>
      <div className={styles.stack}>
        <p className={styles.sub}>{intl.formatMessage({ id: 'credits.standee.body' }, { store: store.name })}</p>
        {code.data && (
          <p>
            <span className={styles.sub}>{intl.formatMessage({ id: 'credits.standee.shortCode' })} </span>
            <span className={styles.code}>{code.data}</span>
          </p>
        )}
        <div className={styles.actions}>
          <Button icon={Eye} loading={busy === 'preview'} loadingLabel={intl.formatMessage({ id: 'credits.standee.preparing' })} onClick={() => void run('preview')}>
            {intl.formatMessage({ id: 'credits.standee.preview' })}
          </Button>
          <Button icon={Download} loading={busy === 'download'} loadingLabel={intl.formatMessage({ id: 'credits.standee.preparing' })} onClick={() => void run('download')}>
            {intl.formatMessage({ id: 'credits.standee.download' })}
          </Button>
          <Link to="/standee" className="tz-btn">
            {intl.formatMessage({ id: 'credits.standee.open' })}
          </Link>
        </div>
      </div>
    </section>
  );
}
