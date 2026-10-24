import { Button, EmptyState } from '@todayz/ui';
import { Wallet } from 'lucide-react';
import { useIntl } from 'react-intl';
import type { CreditLine } from './api';
import styles from './credits.module.css';
import { lineLabelId, lineSign, signedCredits } from './lines';

interface Props {
  lines: CreditLine[];
  hasMore: boolean;
  loadingMore: boolean;
  onMore: () => void;
}

/** The credit account's lines, newest first. A reference is an order or sale number, never a customer. */
export function CreditLines({ lines, hasMore, loadingMore, onMore }: Props) {
  const intl = useIntl();
  if (lines.length === 0) {
    return (
      <EmptyState
        icon={Wallet}
        title={intl.formatMessage({ id: 'credits.lines.empty.title' })}
        description={intl.formatMessage({ id: 'credits.lines.empty.body' })}
      />
    );
  }
  return (
    <>
      <ul className={styles.lines} aria-label={intl.formatMessage({ id: 'credits.lines.label' })}>
        {lines.map((l) => (
          <li key={l.id} className={styles.line}>
            <div className={styles.lineMain}>
              <span className={styles.lineKind}>{intl.formatMessage({ id: lineLabelId(l.kind) })}</span>
              <span className={styles.sub}>
                {intl.formatDate(l.at, { day: 'numeric', month: 'short', year: 'numeric' })}
                {l.ref && ` · ${intl.formatMessage({ id: `credits.ref.${l.ref.kind}` }, { ref: l.ref.label })}`}
              </span>
            </div>
            <span className={lineSign(l) === 1 ? styles.plus : styles.minus}>
              <span className="tz-ui-sr-only">
                {intl.formatMessage({ id: lineSign(l) === 1 ? 'credits.line.added' : 'credits.line.taken' })}
              </span>
              {signedCredits(l)}
            </span>
          </li>
        ))}
      </ul>
      {hasMore && (
        <div className={styles.more}>
          <Button loading={loadingMore} loadingLabel={intl.formatMessage({ id: 'credits.lines.loading' })} onClick={onMore}>
            {intl.formatMessage({ id: 'credits.lines.more' })}
          </Button>
        </div>
      )}
    </>
  );
}
