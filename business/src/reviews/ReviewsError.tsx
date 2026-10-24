import { useIntl } from 'react-intl';
import { reviewsErrorKey } from './queries';

/** A failed reviews call in words the shop can act on. */
export function ReviewsError({ error }: { error: unknown }) {
  const intl = useIntl();
  if (!error) return null;
  return (
    <p role="alert" className="tz-field-error">
      {intl.formatMessage({ id: `reviews.error.${reviewsErrorKey(error)}` })}
    </p>
  );
}
