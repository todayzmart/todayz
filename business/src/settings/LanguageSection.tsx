import { LOCALES, NATIVE_NAMES, isLocale, type Locale } from '@todayz/i18n';
import { LanguagePicker } from '@todayz/ui';
import { useIntl } from 'react-intl';
import { useLocale } from '../app/locale';

/** B-14 language: every role can pick it; it applies at once and is remembered on this device only. */
export function LanguageSection() {
  const intl = useIntl();
  const { locale, choose } = useLocale();
  const current: Locale = isLocale(locale) ? locale : 'en';
  const names: Record<Locale, string> = {
    en: intl.formatMessage({ id: 'settings.language.name.en' }),
    hi: intl.formatMessage({ id: 'settings.language.name.hi' }),
    kn: intl.formatMessage({ id: 'settings.language.name.kn' }),
    te: intl.formatMessage({ id: 'settings.language.name.te' }),
    ta: intl.formatMessage({ id: 'settings.language.name.ta' }),
  };
  return (
    <section className="tz-card" aria-labelledby="set-language">
      <h2 id="set-language" className="tz-card-title">
        {intl.formatMessage({ id: 'settings.language.title' })}
      </h2>
      <p className="tz-card-desc">{intl.formatMessage({ id: 'settings.language.body' })}</p>
      <LanguagePicker
        label={intl.formatMessage({ id: 'settings.language.group' })}
        options={LOCALES.map((code) => ({ code, native: NATIVE_NAMES[code], name: names[code] }))}
        value={current}
        onChange={(code) => void choose(code as Locale)}
      />
    </section>
  );
}
