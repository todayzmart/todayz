// C-21 restricted state (BK-16, 01 §5.4): after too many missed pickups the account may hold one open
// order at a time until a date. Home and Checkout show the reason and the end date from `/v1/me`; a
// quote or place refused with `ordering_suspended` says the same with the problem's `until`.
import { ApiError } from '@todayz/api-client';
import { useIntl } from 'react-intl';

/** The `until` extension of an `ordering_suspended` problem, when the server sent one. */
export function suspendedUntil(err: unknown): string | undefined {
  if (!(err instanceof ApiError) || err.code !== 'ordering_suspended') return undefined;
  const until = err.problem.extensions?.['until'];
  return typeof until === 'string' ? until : undefined;
}

/** The words for `ordering_suspended`: with the end date when known, else the undated fallback. */
export function useSuspendedText() {
  const intl = useIntl();
  return (until: string | undefined): string =>
    until
      ? intl.formatMessage({ id: 'checkout.problem.ordering_suspended.until' }, { date: formatUntil(intl, until) })
      : intl.formatMessage({ id: 'checkout.problem.ordering_suspended' });
}

export function formatUntil(intl: ReturnType<typeof useIntl>, until: string): string {
  return intl.formatDate(until, { day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit' });
}
