import { useSession } from '@todayz/auth';
import { useQueryClient } from '@tanstack/react-query';
import { Outlet } from '@tanstack/react-router';
import { Button } from '@todayz/ui';
import { Fragment } from 'react';
import { useIntl } from 'react-intl';
import { useTodayOverview } from '../overview/queries';
import { SetupCard } from '../overview/SetupCard';
import { clearCachedPosStore } from '../pos/store';
import { NavLink } from './NavLink';
import { SIDE, TABS } from './nav';
import { useNarrow } from './useNarrow';
import { useNavCounts } from './useNavCounts';
import styles from './shell.module.css';

/** Ends this device's session and drops everything cached for the person who was signed in. */
function SignOut() {
  const intl = useIntl();
  const session = useSession();
  const client = useQueryClient();
  return (
    <div className={styles.signOut}>
      <Button
        variant="ghost"
        block
        onClick={() => {
          // The counter's cached shop and role go first, even when the network call fails (D-179).
          // Unsent sales stay in the outbox: they belong to the shop, not to the person.
          clearCachedPosStore();
          void session.signOut().then(() => client.clear());
        }}
      >
        {intl.formatMessage({ id: 'auth.signOut' })}
      </Button>
    </div>
  );
}

/**
 * Business frame in the bright skin (D-261): grey page, white cards, and from 1024px the dark green
 * sidebar (D-269, the founder's override of the white one; `rail` tokens, shell.module.css), phone
 * bottom tabs below it (BottomNav). Only one of the two is rendered visible.
 * The sidebar carries the counts (`useNavCounts`: Orders from the live New list) and, until setup
 * is complete, the shop setup card at its foot (B-03; owners and managers). Below 1024px the
 * Overview shows that card under its KPIs instead, so it is rendered in one place only.
 */
export function Shell() {
  const intl = useIntl();
  const label = intl.formatMessage({ id: 'shell.nav.label' });
  const narrow = useNarrow();
  const counts = useNavCounts();
  const { data } = useTodayOverview();
  return (
    <div className={styles.app} data-skin="bright">
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
              <NavLink item={item} count={counts[item.section]} />
            </Fragment>
          );
        })}
        {!narrow && data && <SetupCard setup={data.setup} />}
        {!narrow && <SignOut />}
      </nav>
      <main id="main" className={styles.main} tabIndex={-1} data-density={narrow ? 'touch' : undefined}>
        <Outlet />
        {narrow && <SignOut />}
      </main>
      <nav className={`tz-bnav ${styles.bnav}`} aria-label={label} data-density="touch">
        {TABS.map((item) => (
          <NavLink key={item.section} item={item} />
        ))}
      </nav>
    </div>
  );
}
