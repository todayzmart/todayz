import { createFileRoute } from '@tanstack/react-router';
import { useIntl } from 'react-intl';
import { SoonCard } from '../shell/Placeholder';

export const Route = createFileRoute('/')({
  component: Pulse,
});

/** A-02 Pulse placeholder. Its WP replaces this file. */
function Pulse() {
  const intl = useIntl();
  return (
    <>
      <div>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.pulse' })}</h1>
        <p className="tz-page-sub">{intl.formatMessage({ id: 'shell.page.sub' })}</p>
      </div>
      <SoonCard section={intl.formatMessage({ id: 'shell.nav.pulse' })} />
    </>
  );
}
