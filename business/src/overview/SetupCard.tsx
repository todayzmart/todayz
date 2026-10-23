import { Link } from '@tanstack/react-router';
import { useIntl } from 'react-intl';
import type { SetupProgress, SetupStep } from './types';
import styles from './overview.module.css';

const HINT: Record<SetupStep, string> = {
  openingHours: 'overview.setup.next.openingHours',
};

/**
 * Shop setup card (B-03: "Setup checklist card until complete"). Desktop: the bottom of the sidebar
 * (`.tz-side-card` sits there with `margin-top: auto`). Phone: under the KPI strip. Hidden at 100%.
 * The bar is decorative; the title already says the percentage.
 */
export function SetupCard({ setup }: { setup: SetupProgress }) {
  const intl = useIntl();
  if (!(setup.percent < 100)) return null; // also hides a missing or NaN percent
  const pct = Math.max(0, Math.floor(setup.percent));
  return (
    <div className="tz-side-card">
      <b>{intl.formatMessage({ id: 'overview.setup.title' }, { pct })}</b>
      <div className="tz-progress" aria-hidden="true">
        <i style={{ width: `${pct}%` }} />
      </div>
      {setup.next && <span>{intl.formatMessage({ id: HINT[setup.next] })}</span>}
      <Link to="/$section" params={{ section: 'settings' }} className={styles.setupLink}>
        {intl.formatMessage({ id: 'overview.setup.finish' })}
      </Link>
    </div>
  );
}
