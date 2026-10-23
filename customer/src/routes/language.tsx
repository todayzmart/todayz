import { LOCALES, NATIVE_NAMES, isLocale, type Locale } from '@todayz/i18n';
import { createFileRoute, useRouter } from '@tanstack/react-router';
import { Button, LanguagePicker } from '@todayz/ui';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { useLocale } from '../app/locale';
import { getPlace } from '../app/place';
import styles from '../shell/shell.module.css';

export const Route = createFileRoute('/language')({
  component: LanguageScreen,
});

/** C-01 language picker: shown once on first run, and again from Account. A card applies at once. */
function LanguageScreen() {
  const intl = useIntl();
  const router = useRouter();
  const { locale, choose } = useLocale();
  const [busy, setBusy] = useState(false);
  const current: Locale = isLocale(locale) ? locale : 'en';

  const names: Record<Locale, string> = {
    en: intl.formatMessage({ id: 'lang.name.en' }),
    hi: intl.formatMessage({ id: 'lang.name.hi' }),
    kn: intl.formatMessage({ id: 'lang.name.kn' }),
    te: intl.formatMessage({ id: 'lang.name.te' }),
    ta: intl.formatMessage({ id: 'lang.name.ta' }),
  };

  // Continue and Skip both end the first-run ask: the language on screen is stored as the choice.
  const done = async () => {
    setBusy(true);
    await choose(current);
    router.history.push(getPlace() ? '/' : '/location');
  };

  return (
    <div className={styles.body}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'lang.title' })}</h1>
      <p className={styles.heroSub}>{intl.formatMessage({ id: 'lang.sub' })}</p>
      <LanguagePicker
        label={intl.formatMessage({ id: 'lang.group' })}
        options={LOCALES.map((code) => ({ code, native: NATIVE_NAMES[code], name: names[code] }))}
        value={current}
        onChange={(code) => void choose(code as Locale)}
      />
      <Button variant="primary" block loading={busy} onClick={() => void done()}>
        {intl.formatMessage({ id: 'lang.continue' })}
      </Button>
      <Button variant="ghost" block disabled={busy} onClick={() => void done()}>
        {intl.formatMessage({ id: 'lang.skip' })}
      </Button>
    </div>
  );
}
