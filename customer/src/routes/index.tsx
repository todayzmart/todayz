import { browserStore, hasChosenLocale } from '@todayz/i18n';
import { Link, createFileRoute, redirect } from '@tanstack/react-router';
import { MapPin } from 'lucide-react';
import { useIntl } from 'react-intl';
import { usePlace } from '../app/place';
import { Icon } from '../shell/Icon';
import { SoonCard } from '../shell/Placeholder';
import styles from '../shell/shell.module.css';

export const Route = createFileRoute('/')({
  // First run: ask for the language once (C-01). `?locale=` in the URL is an explicit choice for that visit.
  beforeLoad: () => {
    if (!hasChosenLocale(browserStore()) && !new URLSearchParams(window.location.search).has('locale')) {
      throw redirect({ to: '/language' });
    }
  },
  component: Home,
});

/** C-01 Home placeholder: mint hero wash, brand, promise line. W1 replaces it. */
function Home() {
  const intl = useIntl();
  const place = usePlace();
  const where =
    place?.kind === 'pin'
      ? intl.formatMessage({ id: 'location.current' }, { place: place.pin })
      : place?.kind === 'gps'
        ? intl.formatMessage({ id: 'location.current' }, { place: intl.formatMessage({ id: 'location.gps.label' }).toLowerCase() })
        : intl.formatMessage({ id: 'location.set' });
  return (
    <>
      <header className={`tz-hero-wash ${styles.hero}`}>
        <div className={styles.brandRow}>
          <span className="tz-side-brand">
            <img className={styles.mark} src="/icons/mark-96.png" alt="" width={28} height={28} />
            <span>
              Today<b>Z</b>
            </span>
          </span>
          <Link to="/location" className="tz-btn tz-btn-soft tz-btn-sm">
            <Icon icon={MapPin} />
            {where}
          </Link>
        </div>
        <h1 className={styles.heroTitle}>{intl.formatMessage({ id: 'shell.hero.title' })}</h1>
        <p className={styles.heroSub}>{intl.formatMessage({ id: 'shell.hero.sub' })}</p>
      </header>
      <div className={styles.body}>
        <SoonCard section={intl.formatMessage({ id: 'shell.nav.home' })} />
      </div>
    </>
  );
}
