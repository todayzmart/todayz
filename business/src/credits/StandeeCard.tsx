import { Skeleton } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import styles from './credits.module.css';
import { useShortCode } from './queries';

/**
 * The QR standee: the store short code (the one in the standee URL, D-26) and a link to /standee,
 * where the A5 sheet is drawn and printed in the browser (D-278).
 */
export function StandeeCard({ store }: { store: CurrentStore }) {
  const intl = useIntl();
  const code = useShortCode(store.bid, store.sid);

  return (
    <section className="tz-card" aria-labelledby="cr-standee-title">
      <div className="tz-card-head">
        <h2 className="tz-card-title" id="cr-standee-title">
          {intl.formatMessage({ id: 'credits.standee.title' })}
        </h2>
      </div>
      <div className={styles.stack}>
        <p className={styles.sub}>{intl.formatMessage({ id: 'credits.standee.body' }, { store: store.name })}</p>

        {code.isPending && <Skeleton width="10ch" />}
        {code.data && (
          <p>
            <span className={styles.sub}>{intl.formatMessage({ id: 'credits.standee.shortCode' })} </span>
            <span className={styles.code}>{code.data}</span>
          </p>
        )}
        <div className={styles.actions}>
          <Link to="/standee" className="tz-btn">
            {intl.formatMessage({ id: 'credits.standee.open' })}
          </Link>
        </div>
      </div>
    </section>
  );
}
