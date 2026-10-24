import type { OfferPreview as Preview } from '@todayz/api-client/gen/model/offerPreview';
import { formatMoney } from '@todayz/i18n';
import { Skeleton } from '@todayz/ui';
import { useIntl } from 'react-intl';
import styles from './offers.module.css';

const money = (paise: number) => formatMoney(paise, 'product');

export interface OfferPreviewProps {
  /** Why there is nothing to show yet: the draft is not valid, or there are no items to price. */
  waiting: boolean;
  pending: boolean;
  failed: boolean;
  preview: Preview | undefined;
  names: ReadonlyMap<string, string>;
}

/** A phone-shaped card with a sample cart priced by the server with the unsaved offer (same code as checkout). */
export function OfferPreview({ waiting, pending, failed, preview, names }: OfferPreviewProps) {
  const intl = useIntl();
  return (
    <section className={styles.phone} aria-label={intl.formatMessage({ id: 'offers.preview.label' })}>
      <h3 className={styles.phoneTitle}>{intl.formatMessage({ id: 'offers.preview.title' })}</h3>
      {waiting ? (
        <p className={styles.note}>{intl.formatMessage({ id: 'offers.preview.waiting' })}</p>
      ) : failed ? (
        <p className={styles.note}>{intl.formatMessage({ id: 'offers.preview.failed' })}</p>
      ) : pending || !preview ? (
        <Skeleton height="5rem" />
      ) : (
        <>
          <ul className={styles.cart}>
            {preview.lines.map((l) => (
              <li key={l.storeItemId} className={styles.cartRow}>
                <span className={styles.grow}>
                  {names.get(l.storeItemId) ?? intl.formatMessage({ id: 'offers.preview.item' })} × {l.quantity}
                  {l.freeUnits > 0 && <span className={styles.free}> {intl.formatMessage({ id: 'offers.preview.free' }, { count: l.freeUnits })}</span>}
                </span>
                {l.discountPaise > 0 && <s className={styles.was}>{money(l.grossPaise)}</s>}
                <strong className="tz-num">{money(l.totalPaise)}</strong>
              </li>
            ))}
          </ul>
          <dl className={styles.totals}>
            <div className={styles.totalRow}>
              <dt>{intl.formatMessage({ id: 'offers.preview.without' })}</dt>
              <dd className="tz-num">{money(preview.totalWithoutPaise)}</dd>
            </div>
            <div className={styles.totalRow}>
              <dt>{intl.formatMessage({ id: 'offers.preview.with' })}</dt>
              <dd className="tz-num">{money(preview.totalPaise)}</dd>
            </div>
          </dl>
          {!preview.applies && <p className={styles.note}>{intl.formatMessage({ id: 'offers.preview.noEffect' })}</p>}
        </>
      )}
    </section>
  );
}
