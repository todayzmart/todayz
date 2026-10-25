import { useState } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { RouterLink } from '../overview/RouterLink';
import { CoachCardRow } from './CoachCardRow';
import { useCoachPreview } from './queries';
import styles from './coach.module.css';

/**
 * The newest live coach cards at the foot of B-03 Overview. Nothing renders while it loads, when it
 * fails or when there is no card, so the Overview never waits on the coach.
 */
export function CoachPreview({ store }: { store: CurrentStore }) {
  const intl = useIntl();
  const q = useCoachPreview(store);
  const [gone, setGone] = useState<ReadonlySet<string>>(() => new Set());
  const cards = (q.data?.items ?? []).filter((c) => !gone.has(c.id));
  if (cards.length === 0) return null;
  return (
    <section className={`tz-card ${styles.preview}`} aria-labelledby="tz-coach-preview">
      <div className="tz-card-head">
        <div>
          <h2 className="tz-card-title" id="tz-coach-preview">
            {intl.formatMessage({ id: 'coach.title' })}
          </h2>
          <p className="tz-card-desc">{intl.formatMessage({ id: 'coach.desc' })}</p>
        </div>
      </div>
      <ul className={`tz-insights ${styles.list}`}>
        {cards.map((c) => (
          <CoachCardRow key={c.id} store={store} card={c} onGone={(id) => setGone((g) => new Set(g).add(id))} />
        ))}
      </ul>
      <div className="tz-card-foot">
        <RouterLink href="/coach">{intl.formatMessage({ id: 'coach.all' })}</RouterLink>
        <span>{intl.formatMessage({ id: 'coach.foot' })}</span>
      </div>
    </section>
  );
}
