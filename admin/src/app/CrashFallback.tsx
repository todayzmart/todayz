import { useRouter, type ErrorComponentProps } from '@tanstack/react-router';
import { Button } from '@todayz/ui';
import { useIntl } from 'react-intl';

export function CrashFallback({ reset }: { reset: () => void }) {
  const intl = useIntl();
  return (
    <div role="alert" style={{ padding: 'var(--space-4)' }}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'app.error.title' })}</h1>
      <p>{intl.formatMessage({ id: 'app.error.body' })}</p>
      <Button variant="primary" onClick={reset}>
        {intl.formatMessage({ id: 'app.error.retry' })}
      </Button>
    </div>
  );
}

/**
 * A page that throws while rendering. The router catches it before the app's ErrorBoundary would,
 * so without this it shows its own untranslated message; this keeps the sidebar and reloads the page.
 */
export function RouteError({ reset }: ErrorComponentProps) {
  const router = useRouter();
  return (
    <CrashFallback
      reset={() => {
        reset();
        void router.invalidate();
      }}
    />
  );
}
