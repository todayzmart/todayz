import { useIntl } from 'react-intl';
import { stockErrorKey } from './action';

/** A failed stock call in words the shop can act on. */
export function StockError({ error }: { error: unknown }) {
  const intl = useIntl();
  if (!error) return null;
  return (
    <p role="alert" className="tz-field-error">
      {intl.formatMessage({ id: `stock.error.${stockErrorKey(error)}` })}
    </p>
  );
}
