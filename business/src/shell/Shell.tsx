import { Outlet } from '@tanstack/react-router';
import { Fragment } from 'react';
import { useIntl } from 'react-intl';
import { NavLink } from './NavLink';
import { SIDE, TABS } from './nav';
import { useNarrow } from './useNarrow';
import styles from './shell.module.css';

/**
 * Business frame: white sidebar with the mint active item from 1024px (Sidebar in the design
 * system), phone bottom tabs below it (BottomNav). Only one of the two is rendered visible.
 */
export function Shell() {
  const intl = useIntl();
  const label = intl.formatMessage({ id: 'shell.nav.label' });
  const narrow = useNarrow();
  return (
    <div className={styles.app}>
      <a className={styles.skip} href="#main">
        {intl.formatMessage({ id: 'shell.skipToContent' })}
      </a>
      <nav className={`tz-side ${styles.side}`} aria-label={label}>
        <div className="tz-side-brand">
          <img className={styles.mark} src="/icons/mark-96.png" alt="" width={24} height={24} />
          <span>
            Today<b>Z</b>
          </span>
          <span className="tz-side-tag">{intl.formatMessage({ id: 'shell.appTag' })}</span>
        </div>
        {SIDE.map((item, i) => {
          const heading = item.group && item.group !== SIDE[i - 1]?.group ? item.group : null;
          return (
            <Fragment key={item.section}>
              {heading && <div className="tz-side-group">{intl.formatMessage({ id: heading })}</div>}
              <NavLink item={item} />
            </Fragment>
          );
        })}
      </nav>
      <main id="main" className={styles.main} tabIndex={-1} data-density={narrow ? 'touch' : undefined}>
        <Outlet />
      </main>
      <nav className={`tz-bnav ${styles.bnav}`} aria-label={label} data-density="touch">
        {TABS.map((item) => (
          <NavLink key={item.section} item={item} />
        ))}
      </nav>
    </div>
  );
}
