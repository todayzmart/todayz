import { Hammer } from 'lucide-react';
import { useIntl } from 'react-intl';
import { Icon } from './Icon';
import styles from './shell.module.css';

/** "Coming soon" card shown for sections whose WP has not landed yet. */
export function SoonCard({ section }: { section: string }) {
  const intl = useIntl();
  return (
    <section className={`tz-card ${styles.soon}`} aria-labelledby="soon-title">
      <span className="tz-iconring">
        <Icon icon={Hammer} />
      </span>
      <span className="tz-tag">{intl.formatMessage({ id: 'shell.soon.tag' })}</span>
      <h2 id="soon-title" className="tz-card-title">
        {intl.formatMessage({ id: 'shell.soon.title' }, { section })}
      </h2>
      <p className="tz-card-desc">{intl.formatMessage({ id: 'shell.soon.body' })}</p>
    </section>
  );
}
