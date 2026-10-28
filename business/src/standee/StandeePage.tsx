import { Banner, Button, EmptyState, Skeleton } from '@todayz/ui';
import { Inbox, Lock, Printer, Store as StoreIcon } from 'lucide-react';
import { Link } from '@tanstack/react-router';
import { useQueryClient } from '@tanstack/react-query';
import { useIntl } from 'react-intl';
import { canSeeCredits } from '../credits/permissions';
import { useShortCode } from '../credits/queries';
import { useCurrentStore, type CurrentStore } from '../orders/store';
import { Campaigns } from './campaigns/Campaigns';
import { noteSheetPrinted } from './campaigns/queries';
import styles from './standee.module.css';
import { storeLink } from './link';
import { StandeeSheet } from './StandeeSheet';
import { usePrintSheet } from './usePrintSheet';

/**
 * QR standee (06 B-02 step and B-10 link, D-51). W2-10 owns the sheet, drawn and printed in the browser (D-278); campaigns,
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
          action={<Button loading={current.retrying} onClick={current.retry}>{intl.formatMessage({ id: 'standee.retry' })}</Button>}
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
  const qc = useQueryClient();
  const { print, portal } = usePrintSheet();
  // The store standee's link (D-26): the scan opens the shop and counts as a store referral.
  const sheet = code.data ? { shop: store.name, link: storeLink(code.data), code: code.data } : null;

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
          {code.isPending && <Skeleton width="10ch" />}
          {code.isError && (
            <Banner tone="warning" action={<Button loading={code.isFetching} onClick={() => void code.refetch()}>{intl.formatMessage({ id: 'standee.retry' })}</Button>}>
              {intl.formatMessage({ id: 'standee.codeError' })}
            </Banner>
          )}
          {sheet && (
            <>
              <p>
                <span className={styles.sub}>{intl.formatMessage({ id: 'standee.shortCode' })} </span>
                <span className={styles.code}>{sheet.code}</span>
              </p>
              <div className={styles.actions}>
                <Button variant="primary" icon={Printer} onClick={() => {
                    print(sheet);
                    noteSheetPrinted(qc, store);
                  }}
                >
                  {intl.formatMessage({ id: 'standee.print' })}
                </Button>
              </div>
              <figure className={styles.preview} aria-label={intl.formatMessage({ id: 'standee.frameTitle' })}>
                <StandeeSheet {...sheet} />
              </figure>
            </>
          )}
          <p className={styles.sub}>{intl.formatMessage({ id: 'standee.printTip' })}</p>
        </div>
      </section>
      <Campaigns store={store} shortCode={code.data} onPrint={print} />
      {portal}
    </>
  );
}
