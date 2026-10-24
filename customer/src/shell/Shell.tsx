import { OfflineBanner } from '@todayz/auth';
import { Link, Outlet, type LinkProps } from '@tanstack/react-router';
import { useIntl } from 'react-intl';
import { Icon } from './Icon';
import { NAV } from './nav';
import styles from './shell.module.css';

/** Phone-first frame: content column plus the fixed bottom nav (BottomNav in the design system). */
export function Shell() {
  const intl = useIntl();
  return (
    <div className={styles.app} data-density="touch">
      <a className={styles.skip} href="#main">
        {intl.formatMessage({ id: 'shell.skipToContent' })}
      </a>
      <OfflineBanner
        className={styles.offline}
        title={intl.formatMessage({ id: 'app.offline.title' })}
        message={intl.formatMessage({ id: 'app.offline.body' })}
      />
      <main id="main" className={styles.main} tabIndex={-1}>
        <Outlet />
      </main>
      <nav className={`tz-bnav ${styles.bnav}`} aria-label={intl.formatMessage({ id: 'shell.nav.label' })}>
        {NAV.map((item) =>
          item.section === '' ? (
            <Link key="home" to="/" activeOptions={{ exact: true }}>
              <Icon icon={item.icon} />
              {intl.formatMessage({ id: item.label })}
            </Link>
          ) : (
            // The plain path, not the `/$section` template: a built section's static route wins.
            <Link key={item.section} to={`/${item.section}` as NonNullable<LinkProps['to']>}>
              <Icon icon={item.icon} />
              {intl.formatMessage({ id: item.label })}
            </Link>
          ),
        )}
      </nav>
    </div>
  );
}
