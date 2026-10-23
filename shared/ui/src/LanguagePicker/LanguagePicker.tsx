import { cx } from '../internal/cx';

export interface LanguageOption {
  /** BCP 47 code, e.g. "kn". Sets lang on the native name so the right font cut renders. */
  code: string;
  /** The language's own name, e.g. "ಕನ್ನಡ". */
  native: string;
  /** Its name in the current interface language, e.g. "Kannada". */
  name: string;
}

export interface LanguagePickerProps {
  /** Accessible name of the group, e.g. "Language". */
  label: string;
  options: readonly LanguageOption[];
  value: string;
  onChange: (code: string) => void;
  className?: string | undefined;
}

/** Language cards (tz-lang): native name large, interface-language name below. */
export function LanguagePicker({ label, options, value, onChange, className }: LanguagePickerProps) {
  return (
    <div className={cx('tz-lang', className)} role="group" aria-label={label}>
      {options.map((o) => (
        <button
          key={o.code}
          type="button"
          aria-pressed={o.code === value}
          onClick={() => o.code !== value && onChange(o.code)}
        >
          <span className="native" lang={o.code}>
            {o.native}
          </span>
          <small>{o.name}</small>
        </button>
      ))}
    </div>
  );
}
