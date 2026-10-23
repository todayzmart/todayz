import { Search, X } from 'lucide-react';
import {
  forwardRef,
  useEffect,
  useId,
  useImperativeHandle,
  useRef,
  useState,
  type InputHTMLAttributes,
  type ReactNode,
} from 'react';
import { cx } from '../internal/cx';
import { Icon } from '../internal/Icon';

export interface SearchFieldProps extends Omit<
  InputHTMLAttributes<HTMLInputElement>,
  'onChange' | 'value' | 'defaultValue' | 'type'
> {
  /** Accessible name of the input (the field shows only the placeholder). */
  label: string;
  value?: string | undefined;
  defaultValue?: string | undefined;
  /** Every keystroke. */
  onChange?: (value: string) => void;
  /**
   * Debounced query: fires `debounceMs` after typing stops, once the text has `minChars`
   * characters, and with '' when the field is cleared (README SearchField: 200 ms after 2).
   */
  onSearch?: (query: string) => void;
  debounceMs?: number | undefined;
  minChars?: number | undefined;
  /** Trailing slot, e.g. a voice-search icon button. */
  trailing?: ReactNode;
  /** Label of the clear button; without it there is no clear button. */
  clearLabel?: string | undefined;
  className?: string | undefined;
}

/** Search input with a leading search icon (tz-search). The ref is the input. */
export const SearchField = forwardRef<HTMLInputElement, SearchFieldProps>(function SearchField(
  {
  label,
  value,
  defaultValue = '',
  onChange,
  onSearch,
  debounceMs = 200,
  minChars = 2,
  trailing,
  clearLabel,
  className,
    id,
    ...rest
  },
  ref,
) {
  const auto = useId();
  const inputId = id ?? `tz-search-${auto}`;
  const [inner, setInner] = useState(defaultValue);
  const text = value ?? inner;
  const searchRef = useRef(onSearch);
  const inputRef = useRef<HTMLInputElement>(null);
  useImperativeHandle(ref, () => inputRef.current as HTMLInputElement, []);
  const first = useRef(true);
  useEffect(() => {
    searchRef.current = onSearch;
  });

  useEffect(() => {
    if (first.current) {
      first.current = false;
      return undefined;
    }
    const q = text.trim();
    if (q.length > 0 && q.length < minChars) return undefined;
    const t = setTimeout(() => searchRef.current?.(q), debounceMs);
    return () => clearTimeout(t);
  }, [text, minChars, debounceMs]);

  const update = (next: string) => {
    if (value === undefined) setInner(next);
    onChange?.(next);
  };

  return (
    // A div, not a label: a label may not hold buttons (clear, voice) besides its own control.
    <div className={cx('tz-search', className)}>
      <Icon icon={Search} />
      <input
        ref={inputRef}
        id={inputId}
        type="search"
        aria-label={label}
        value={text}
        onChange={(e) => update(e.target.value)}
        {...rest}
      />
      {clearLabel && text.length > 0 && (
        <button
          type="button"
          className="tz-ui-search-clear"
          aria-label={clearLabel}
          onClick={() => {
            update('');
            inputRef.current?.focus();
          }}
        >
          <Icon icon={X} small />
        </button>
      )}
      {trailing && <span className="tz-ui-search-trailing">{trailing}</span>}
    </div>
  );
});
