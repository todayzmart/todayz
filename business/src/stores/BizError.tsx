import { useIntl } from 'react-intl';
import { bizErrorKey } from './queries';

/** A failed save in words the shop can act on (`stores.error.*`). */
export function BizError({ error }: { error: unknown }) {
  const intl = useIntl();
  if (!error) return null;
  return (
    <p role="alert" className="tz-field-error">
      {intl.formatMessage({ id: `stores.error.${bizErrorKey(error)}` })}
    </p>
  );
}
