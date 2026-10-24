import { useIntl } from 'react-intl';
import styles from './credits.module.css';

/** How payouts work (01 §9), plain text from i18n. */
export function PayoutsCard() {
  const intl = useIntl();
  const ids = ['in', 'out', 'statement', 'amount', 'paid', 'holds'] as const;
  return (
    <section className="tz-card" aria-labelledby="cr-payouts-title">
      <div className="tz-card-head">
        <h2 className="tz-card-title" id="cr-payouts-title">
          {intl.formatMessage({ id: 'credits.payouts.title' })}
        </h2>
      </div>
      <ul className={styles.explain}>
        {ids.map((id) => (
          <li key={id}>{intl.formatMessage({ id: `credits.payouts.${id}` })}</li>
        ))}
      </ul>
    </section>
  );
}
