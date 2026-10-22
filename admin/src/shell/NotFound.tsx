import { Link } from '@tanstack/react-router';
import { MapPinOff } from 'lucide-react';
import { useIntl } from 'react-intl';
import { Icon } from './Icon';
import styles from './shell.module.css';

export function NotFound() {
  const intl = useIntl();
  return (
    <div className={styles.page}>
      <section className={`tz-card ${styles.soon}`} aria-labelledby="nf-title">
        <span className="tz-iconring">
          <Icon icon={MapPinOff} />
        </span>
        <h1 id="nf-title" className="tz-page-title">
          {intl.formatMessage({ id: 'shell.notFound.title' })}
        </h1>
        <p className="tz-card-desc">{intl.formatMessage({ id: 'shell.notFound.body' })}</p>
        <Link to="/" className="tz-btn tz-btn-primary">
          {intl.formatMessage({ id: 'shell.notFound.home' })}
        </Link>
      </section>
    </div>
  );
}
