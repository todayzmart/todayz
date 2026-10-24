import { Star } from 'lucide-react';
import { useIntl } from 'react-intl';
import styles from './reviews.module.css';

/** Five stars, `value` filled. The text name ("4 out of 5 stars") is the accessible name; the glyphs are decorative. */
export function Stars({ value }: { value: number }) {
  const intl = useIntl();
  const full = Math.round(value);
  return (
    <span className={styles.stars} role="img" aria-label={intl.formatMessage({ id: 'reviews.stars' }, { n: value })}>
      {[1, 2, 3, 4, 5].map((n) => (
        <Star key={n} className={n <= full ? styles.starOn : styles.starOff} size={16} strokeWidth={1.5} aria-hidden="true" focusable="false" />
      ))}
    </span>
  );
}
