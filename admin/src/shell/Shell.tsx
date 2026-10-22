import { Link, Outlet } from '@tanstack/react-router';
import { Fragment } from 'react';
import { useIntl } from 'react-intl';
import { Icon } from './Icon';
import { NAV, type NavItem } from './nav';
import styles from './shell.module.css';

/**
 * One sidebar entry. The visible label is the accessible name (visually hidden on the icon rail);
 * `title` gives the rail a hover tooltip.
 */
function NavLink({ item }: { item: NavItem }) {
  const intl = useIntl();
  const text = intl.formatMessage({ id: item.label });
  const label = <span className={styles.label}>{text}</span>;
  return item.section === '' ? (
    <Link to="/" activeOptions={{ exact: true }} title={text}>
      <Icon icon={item.icon} />
      {label}
    </Link>
  ) : (
    <Link to="/$section" params={{ section: item.section }} title={text}>
      <Icon icon={item.icon} />
      {label}
    </Link>
  );
}

/**
 * Admin frame: the business app's white sidebar plus the "Admin" tag, so staff always know which
 * app they are in. Below 1024px the sidebar collapses to icons (labels stay for screen readers).
 */
export function Shell() {
  const intl = useIntl();
  return (
    <div className={styles.app}>
      <a className={styles.skip} href="#main">
        {intl.formatMessage({ id: 'shell.skipToContent' })}
      </a>
      <nav className={`tz-side ${styles.side}`} aria-label={intl.formatMessage({ id: 'shell.nav.label' })}>
        <div className="tz-side-brand">
          <img className={styles.mark} src="/icons/mark-96.png" alt="" width={24} height={24} />
          <span className={styles.label}>
            Today<b>Z</b>
          </span>
          <span className={`tz-side-tag ${styles.tag}`}>{intl.formatMessage({ id: 'shell.appTag' })}</span>
        </div>
        {NAV.map((item, i) => {
          const heading = item.group && item.group !== NAV[i - 1]?.group ? item.group : null;
          return (
            <Fragment key={item.section || 'pulse'}>
              {heading && (
                <div className={`tz-side-group ${styles.group}`}>
                  <span className={styles.label}>{intl.formatMessage({ id: heading })}</span>
                </div>
              )}
              <NavLink item={item} />
            </Fragment>
          );
        })}
      </nav>
      <main id="main" className={styles.main} tabIndex={-1}>
        <Outlet />
      </main>
    </div>
  );
}
