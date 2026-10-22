import { createFileRoute } from '@tanstack/react-router';
import { useIntl } from 'react-intl';
import { SoonCard } from '../shell/Placeholder';
import styles from '../shell/shell.module.css';

export const Route = createFileRoute('/')({
  component: Home,
});

/** C-01 Home placeholder: mint hero wash, brand, promise line. W1 replaces it. */
function Home() {
  const intl = useIntl();
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
