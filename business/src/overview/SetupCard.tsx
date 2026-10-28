import { Circle, CircleCheck, X } from 'lucide-react';
import { useState } from 'react';
import type { IntlShape } from 'react-intl';
import { useIntl } from 'react-intl';
import { RouterLink } from './RouterLink';
import { SETUP_STEPS, type SetupProgress, type SetupStep } from './types';
import styles from './overview.module.css';

/** Where each step is done. */
const HREF: Record<SetupStep, string> = {
  kyc: '/onboarding',
  products: '/stock',
  staff: '/stores',
  standee: '/standee',
  openingHours: '/settings',
};

function hint(intl: IntlShape, step: SetupStep): string {
  switch (step) {
    case 'kyc':
      return intl.formatMessage({ id: 'overview.setup.next.kyc' });
    case 'products':
      return intl.formatMessage({ id: 'overview.setup.next.products' });
    case 'staff':
      return intl.formatMessage({ id: 'overview.setup.next.staff' });
    case 'standee':
      return intl.formatMessage({ id: 'overview.setup.next.standee' });
    case 'openingHours':
      return intl.formatMessage({ id: 'overview.setup.next.openingHours' });
  }
}

function stepLabel(intl: IntlShape, step: SetupStep): string {
  switch (step) {
    case 'kyc':
      return intl.formatMessage({ id: 'overview.setup.step.kyc' });
    case 'products':
      return intl.formatMessage({ id: 'overview.setup.step.products' });
    case 'staff':
      return intl.formatMessage({ id: 'overview.setup.step.staff' });
    case 'standee':
      return intl.formatMessage({ id: 'overview.setup.step.standee' });
    case 'openingHours':
      return intl.formatMessage({ id: 'overview.setup.step.openingHours' });
  }
}

const HIDDEN = (storeId: string) => `todayz.setupHidden.${storeId}`;

function isHidden(storeId: string): boolean {
  try {
    return localStorage.getItem(HIDDEN(storeId)) === '1';
  } catch {
    return false;
  }
}

function rememberHidden(storeId: string): void {
  try {
    localStorage.setItem(HIDDEN(storeId), '1');
  } catch {
    // Private mode: the card is hidden until the next load.
  }
}

/**
 * Shop setup card (B-03: "Setup checklist card until complete"). Desktop: the bottom of the sidebar
 * (`.tz-side-card` sits there with `margin-top: auto`). Phone: under the KPI strip. Hidden at 100%.
 * The bar is decorative; the title already says the percentage. Every step is listed with a tick
 * or an open circle, and an open step links to where it is done; "Finish setup" opens the next one.
 * The × hides the card for this store on this device (founder, 28 Oct); the empty Demand insights
 * card still lists the steps left.
 */
export function SetupCard({ setup, storeId }: { setup: SetupProgress; storeId: string }) {
  const intl = useIntl();
  const [hidden, setHidden] = useState(() => isHidden(storeId));
  if (!(setup.percent < 100) || hidden) return null; // also hides a missing or NaN percent
  const pct = Math.max(0, Math.floor(setup.percent));
  const next = setup.next ?? SETUP_STEPS.find((s) => !setup.steps[s]) ?? null;
  const hide = () => {
    rememberHidden(storeId);
    setHidden(true);
  };
  return (
    <div className="tz-side-card">
      <div className={styles.setupHead}>
        <b>{intl.formatMessage({ id: 'overview.setup.title' }, { pct })}</b>
        <button
          type="button"
          className={styles.setupHide}
          aria-label={intl.formatMessage({ id: 'overview.setup.hide' })}
          onClick={hide}
        >
          <X size={16} aria-hidden="true" />
        </button>
      </div>
      <div className="tz-progress" aria-hidden="true">
        <i style={{ width: `${pct}%` }} />
      </div>
      <ul className={styles.setupSteps} aria-label={intl.formatMessage({ id: 'overview.setup.steps' })}>
        {SETUP_STEPS.map((s) =>
          setup.steps[s] ? (
            <li key={s} data-done="">
              <CircleCheck size={16} aria-hidden="true" />
              <span>
                {stepLabel(intl, s)}
                <span className="tz-ui-sr-only">, {intl.formatMessage({ id: 'overview.setup.done' })}</span>
              </span>
            </li>
          ) : (
            <li key={s}>
              <Circle size={16} aria-hidden="true" />
              <RouterLink href={HREF[s]}>{stepLabel(intl, s)}</RouterLink>
            </li>
          ),
        )}
      </ul>
      {next && <span>{hint(intl, next)}</span>}
      <RouterLink href={next ? HREF[next] : '/settings'} className={styles.setupLink}>
        {intl.formatMessage({ id: 'overview.setup.finish' })}
      </RouterLink>
    </div>
  );
}

/** The steps left, each a link to where it is done: the empty Demand insights card shows it (D-99). */
export function SetupChecklist({ setup }: { setup: SetupProgress }) {
  const intl = useIntl();
  const left = SETUP_STEPS.filter((s) => !setup.steps[s]);
  if (left.length === 0) return null;
  return (
    <ul className={styles.checklist} aria-label={intl.formatMessage({ id: 'overview.setup.left' })}>
      {left.map((s) => (
        <li key={s}>
          <RouterLink href={HREF[s]}>{stepLabel(intl, s)}</RouterLink>
        </li>
      ))}
    </ul>
  );
}
