import type { StandeeCounts } from '@todayz/api-client/gen/model/standeeCounts';
import { useIntl } from 'react-intl';
import styles from '../standee.module.css';

/**
 * Scans → sign-ups → qualified referrals (B-10, SC-ST-04). A scan is an open of the standee's QR
 * link, a sign-up a customer who joined from it (rejected ones are not counted), qualified one whose
 * first qualifying purchase is done (D-223).
 */
export function Funnel({ counts, label }: { counts: StandeeCounts; label: string }) {
  const intl = useIntl();
  const steps = [
    { key: 'scans', value: counts.scans },
    { key: 'signUps', value: counts.signUps },
    { key: 'qualified', value: counts.qualified },
  ] as const;
  return (
    <dl className={styles.funnel} aria-label={label}>
      {steps.map((s) => (
        <div key={s.key} className={styles.step}>
          <dt className={styles.sub}>{intl.formatMessage({ id: `standee.funnel.${s.key}` })}</dt>
          <dd className={styles.count}>{intl.formatNumber(s.value)}</dd>
        </div>
      ))}
    </dl>
  );
}
