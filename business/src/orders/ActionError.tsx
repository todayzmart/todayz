import { useIntl } from 'react-intl';
import { errorKey } from './queries';

/** An action's failure in words the shop can act on. */
export function ActionError({ error }: { error: unknown }) {
  const intl = useIntl();
  if (!error) return null;
  return (
    <p role="alert" className="tz-field-error">
      {intl.formatMessage({ id: `orders.error.${errorKey(error)}` })}
    </p>
  );
}
