import { Link, createFileRoute } from '@tanstack/react-router';
import { useIntl } from 'react-intl';
import styles from '../../shell/shell.module.css';

export const Route = createFileRoute('/invite/$token')({
  component: Invite,
});

/**
 * B-01 invite acceptance for staff. Signed-out visitors are sent to sign in first and come back here.
 * TODO(W1-04): accept the invitation once the staff invite endpoints exist; the token stays in the URL.
 */
function Invite() {
  const intl = useIntl();
  return (
    <section className={`tz-card ${styles.stack}`} aria-labelledby="invite-title">
      <h1 id="invite-title" className="tz-page-title">
        {intl.formatMessage({ id: 'shop.invite.title' })}
      </h1>
      <p className="tz-card-desc">{intl.formatMessage({ id: 'shop.invite.soon' })}</p>
      <Link to="/" className="tz-btn tz-btn-primary">
        {intl.formatMessage({ id: 'shop.invite.home' })}
      </Link>
    </section>
  );
}
