import { Banner, Button, EmptyState, Skeleton } from '@todayz/ui';
import { Download, Inbox, Lock, Printer, Store as StoreIcon } from 'lucide-react';
import { Link } from '@tanstack/react-router';
import { useEffect, useState } from 'react';
import { useIntl } from 'react-intl';
import { getStandeePdf } from '../credits/api';
import { canSeeCredits } from '../credits/permissions';
import { useShortCode } from '../credits/queries';
import { useCurrentStore, type CurrentStore } from '../orders/store';
import { Campaigns } from './campaigns/Campaigns';
import styles from './standee.module.css';

/**
 * QR standee (06 B-02 step and B-10 link, D-51). W2-10 owns the PDF and the preview; campaigns,
 * reorders and scan analytics are W3-09 (`campaigns/`, `analytics/`). Manager and above; the API is the real gate.
 */
export function StandeePage() {
  const intl = useIntl();
  const current = useCurrentStore();
  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'standee.title' })}</h1>
      {current.status === 'loading' && (
        <div role="status" aria-label={intl.formatMessage({ id: 'standee.loading' })}>
          <Skeleton height="8rem" />
        </div>
      )}
      {current.status === 'error' && (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'standee.error.title' })}
          description={intl.formatMessage({ id: 'standee.error.body' })}
          action={<Button onClick={current.retry}>{intl.formatMessage({ id: 'standee.retry' })}</Button>}
        />
      )}
      {current.status === 'none' && (
        <EmptyState
          icon={StoreIcon}
          title={intl.formatMessage({ id: 'standee.noStore.title' })}
          description={intl.formatMessage({ id: 'standee.noStore.body' })}
          action={
            <Link to="/choose-shop" search={{ next: '/standee' }} className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'standee.noStore.action' })}
            </Link>
          }
        />
      )}
      {current.status === 'ready' &&
        (canSeeCredits(current.store.role) ? (
          <Standee store={current.store} />
        ) : (
          <EmptyState icon={Lock} title={intl.formatMessage({ id: 'standee.manager.title' })} description={intl.formatMessage({ id: 'standee.manager.body' })} />
        ))}
    </div>
  );
}

function Standee({ store }: { store: CurrentStore }) {
  const intl = useIntl();
  const code = useShortCode(store.bid, store.sid);
  const [pdf, setPdf] = useState<{ url: string } | null>(null);
  const [busy, setBusy] = useState<'preview' | 'download' | null>(null);
  const [failed, setFailed] = useState(false);

  // The blob URL lives only as long as the preview; nothing secret is in it.
  useEffect(() => () => {
    if (pdf) URL.revokeObjectURL(pdf.url);
  }, [pdf]);

  const fetchPdf = async (mode: 'preview' | 'download') => {
    setBusy(mode);
    setFailed(false);
    try {
      const blob = await getStandeePdf(store.bid, store.sid);
      const url = URL.createObjectURL(blob);
      if (mode === 'preview') {
        setPdf({ url });
      } else {
        const a = document.createElement('a');
        a.href = url;
        a.download = 'todayz-standee-a5.pdf';
        a.click();
        window.setTimeout(() => URL.revokeObjectURL(url), 60_000);
      }
    } catch {
      setFailed(true);
    } finally {
      setBusy(null);
    }
  };

  return (
    <>
      <p className={styles.sub}>{store.name}</p>
      <section className="tz-card" aria-labelledby="sd-title">
        <div className="tz-card-head">
          <h2 className="tz-card-title" id="sd-title">
            {intl.formatMessage({ id: 'standee.card.title' })}
          </h2>
        </div>
        <div className={styles.stack}>
          <p className={styles.sub}>{intl.formatMessage({ id: 'standee.card.body' })}</p>
          {code.data && (
            <p>
              <span className={styles.sub}>{intl.formatMessage({ id: 'standee.shortCode' })} </span>
              <span className={styles.code}>{code.data}</span>
            </p>
          )}
          {failed && (
            <Banner tone="warning" action={<Button onClick={() => void fetchPdf(busy ?? 'preview')}>{intl.formatMessage({ id: 'standee.retry' })}</Button>}>
              {intl.formatMessage({ id: 'standee.pdfError' })}
            </Banner>
          )}
          <div className={styles.actions}>
            <Button icon={Printer} loading={busy === 'preview'} loadingLabel={intl.formatMessage({ id: 'standee.preparing' })} onClick={() => void fetchPdf('preview')}>
              {intl.formatMessage({ id: 'standee.preview' })}
            </Button>
            <Button icon={Download} loading={busy === 'download'} loadingLabel={intl.formatMessage({ id: 'standee.preparing' })} onClick={() => void fetchPdf('download')}>
              {intl.formatMessage({ id: 'standee.download' })}
            </Button>
          </div>
          {pdf && <iframe className={styles.frame} title={intl.formatMessage({ id: 'standee.frameTitle' })} src={pdf.url} />}
          <p className={styles.sub}>{intl.formatMessage({ id: 'standee.printTip' })}</p>
        </div>
      </section>
      <Campaigns store={store} />
    </>
  );
}
