import { ApiError } from '@todayz/api-client';
import { useCallback } from 'react';
import { useIntl } from 'react-intl';

/** One sentence for a failed admin change; the problem code picks it, never the server's text. */
export function useProblemText() {
  const intl = useIntl();
  return useCallback(
    (err: unknown): string => {
      if (!(err instanceof ApiError)) return intl.formatMessage({ id: 'common.error.network' });
      switch (err.code) {
        case 'forbidden':
          return intl.formatMessage({ id: 'common.error.forbidden' });
        case 'invalid_transition':
          return intl.formatMessage({ id: 'common.error.moved' });
        case 'not_found':
          return intl.formatMessage({ id: 'common.error.notFound' });
        case 'validation_failed':
          return intl.formatMessage({ id: 'common.error.invalid' });
        default:
          return intl.formatMessage({ id: 'common.error.generic' });
      }
    },
    [intl],
  );
}
