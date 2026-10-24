import type { StoreRating } from '@todayz/api-client/gen/model/storeRating';
import { useIntl } from 'react-intl';
import { Stars } from './Stars';
import styles from './reviews.module.css';

/** The shop's rating: the average and the number of reviews (the API sends no per-star split). */
export function Summary({ rating }: { rating: StoreRating }) {
  const intl = useIntl();
  return (
    <section className={styles.summary} aria-label={intl.formatMessage({ id: 'reviews.summary.label' })}>
      <div className={styles.score}>
        <span className={styles.avg}>{rating.avg === null ? '–' : intl.formatNumber(rating.avg, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}</span>
        {rating.avg !== null && <Stars value={rating.avg} />}
        <span className={styles.sub}>{intl.formatMessage({ id: 'reviews.summary.count' }, { count: rating.count })}</span>
      </div>
    </section>
  );
}
