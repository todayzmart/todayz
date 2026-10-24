import { FileText } from 'lucide-react';
import { useIntl } from 'react-intl';
import styles from './credits.module.css';

/**
 * Monthly statements (status, paid UTR) are W3-01's (`business/src/statements/`, D-51).
 * TODO(merge): W3-01 replaces this with the statements list.
 */
export function StatementsSlot() {
  const intl = useIntl();
  return (
    <section className="tz-card" aria-labelledby="cr-statements-title">
      <div className="tz-card-head">
        <h2 className="tz-card-title" id="cr-statements-title">
          {intl.formatMessage({ id: 'credits.statements.title' })}
        </h2>
      </div>
      <div className={styles.soon}>
        <FileText size={20} aria-hidden="true" />
        <p>{intl.formatMessage({ id: 'credits.statements.soon' })}</p>
      </div>
    </section>
  );
}
