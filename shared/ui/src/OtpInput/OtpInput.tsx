import {
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ClipboardEvent,
  type KeyboardEvent,
  type ReactNode,
} from 'react';
import { cx } from '../internal/cx';

export interface OtpInputProps {
  /** Label of the whole code: names the group of boxes and the first box (e.g. "Enter customer's code"). */
  label: ReactNode;
  /** Name of boxes 2…n, e.g. (i) => `Digit ${i}`. */
  digitLabel: (position: number) => string;
  length?: number | undefined;
  value: string;
  onChange: (value: string) => void;
  /** Fires once when every box holds a digit; not again for the same code until it is edited. */
  onComplete?: ((code: string) => void) | undefined;
  help?: ReactNode;
  /** Wrong-code message; the boxes shake once each time it changes to a new non-empty value. */
  error?: ReactNode;
  /** Bump to shake again for a repeated wrong code with the same message. */
  errorKey?: number | string | undefined;
  disabled?: boolean | undefined;
  autoFocus?: boolean | undefined;
  className?: string | undefined;
}

const onlyDigits = (s: string) => s.replace(/\D/g, '');

/**
 * Shop-side code entry (tz-otp): one box per digit in a labelled group, auto-advance, Backspace
 * steps back (also on Android keyboards, which send no Backspace key), paste fills every box,
 * arrows move. A wrong code shakes once (a fade when motion is reduced).
 */
export function OtpInput({
  label,
  digitLabel,
  length = 4,
  value,
  onChange,
  onComplete,
  help,
  error,
  errorKey,
  disabled = false,
  autoFocus = false,
  className,
}: OtpInputProps) {
  const id = useId();
  const firstId = `tz-otp-${id}`;
  const msgId = `${firstId}-msg`;
  const labelId = `${firstId}-label`;
  const refs = useRef<(HTMLInputElement | null)[]>([]);
  const digits = Array.from({ length }, (_, i) => value[i] ?? '');
  const invalid = error !== undefined && error !== null && error !== false && error !== '';
  const [shaking, setShaking] = useState(false);
  // Shake once each time an error appears or changes (errorKey re-triggers the same message).
  const [seen, setSeen] = useState<{ invalid: boolean; errorKey: unknown; error: unknown }>({
    invalid: false,
    errorKey: undefined,
    error: undefined,
  });
  if (seen.invalid !== invalid || seen.errorKey !== errorKey || seen.error !== error) {
    setSeen({ invalid, errorKey, error });
    if (invalid) setShaking(true);
  }

  const focusAt = (i: number) => {
    const el = refs.current[Math.max(0, Math.min(length - 1, i))];
    el?.focus();
    el?.select();
  };

  /** The complete code last reported, so a repeat (same paste, same digit retyped) is not. */
  const completed = useRef<string | null>(null);
  useEffect(() => {
    // The parent cleared or shortened the code (a wrong code, a retry): the next full code counts.
    if (value.length < length) completed.current = null;
  }, [value, length]);

  const commit = (next: string) => {
    const clean = onlyDigits(next).slice(0, length);
    onChange(clean);
    if (clean.length < length) {
      completed.current = null;
    } else if (clean !== completed.current) {
      completed.current = clean;
      onComplete?.(clean);
    }
  };

  const setDigit = (i: number, d: string) => {
    const arr = [...digits];
    arr[i] = d;
    // Keep the value contiguous: a digit typed past a gap lands in the first empty box.
    const joined = arr.join('');
    commit(joined);
    return joined.length;
  };

  /** Backspace in box i: clear it, or when it is empty, clear the one before and step back. */
  const backspace = (i: number) => {
    if (digits[i]) {
      commit(digits.slice(0, i).join('') + digits.slice(i + 1).join(''));
      focusAt(i);
    } else if (i > 0) {
      commit(digits.slice(0, i - 1).join('') + digits.slice(i).join(''));
      focusAt(i - 1);
    }
  };
  const backspaceRef = useRef(backspace);
  useLayoutEffect(() => {
    backspaceRef.current = backspace;
  });

  // Android keyboards send keydown "Unidentified" (229), not Backspace, and an empty box has
  // nothing to delete, so no change event: the native beforeinput (React's onBeforeInput does not
  // carry inputType) is the only signal. Desktop Backspace is handled in keydown and never gets here.
  useEffect(() => {
    const boxes = refs.current.slice(0, length);
    const handlers = boxes.map((el, i) => {
      const h = (e: Event) => {
        if ((e as InputEvent).inputType !== 'deleteContentBackward') return;
        e.preventDefault();
        backspaceRef.current(i);
      };
      el?.addEventListener('beforeinput', h);
      return h;
    });
    return () => {
      boxes.forEach((el, i) => {
        const h = handlers[i];
        if (h) el?.removeEventListener('beforeinput', h);
      });
    };
  }, [length]);

  const onInput = (i: number, raw: string) => {
    let typed = onlyDigits(raw);
    if (typed.length === 0) {
      // The box was emptied some other way (cut, a keyboard that deletes without beforeinput).
      if (raw === '' && digits[i]) backspace(i);
      return;
    }
    const old = digits[i];
    if (old && typed.length === 2) {
      // Typed into a filled box without selecting it: keep the new digit only.
      typed = typed[0] === old ? typed.slice(1) : typed.slice(0, 1);
    }
    if (typed.length > 1) {
      // Autofill or a fast typist: treat like a paste from this box.
      const merged = (digits.slice(0, i).join('') + typed).slice(0, length);
      commit(merged);
      focusAt(merged.length);
      return;
    }
    const filled = setDigit(i, typed.slice(-1));
    focusAt(Math.min(filled, length - 1));
  };

  const onKeyDown = (i: number, e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Backspace') {
      e.preventDefault();
      backspace(i);
    } else if (e.key === 'ArrowLeft') {
      e.preventDefault();
      focusAt(i - 1);
    } else if (e.key === 'ArrowRight') {
      e.preventDefault();
      focusAt(i + 1);
    }
  };

  const onPaste = (e: ClipboardEvent<HTMLInputElement>) => {
    const pasted = onlyDigits(e.clipboardData.getData('text'));
    e.preventDefault();
    if (!pasted) return;
    const merged = pasted.slice(0, length);
    commit(merged);
    focusAt(merged.length);
  };

  return (
    <div className={cx('tz-field', className)}>
      <label className="tz-field-label" htmlFor={firstId} id={labelId}>
        {label}
      </label>
      <div
        className={cx('tz-otp', shaking && 'tz-ui-shake')}
        role="group"
        aria-labelledby={labelId}
        onAnimationEnd={() => setShaking(false)}
      >
        {digits.map((d, i) => (
          <input
            key={i}
            ref={(el) => {
              refs.current[i] = el;
            }}
            id={i === 0 ? firstId : undefined}
            aria-label={i === 0 ? undefined : digitLabel(i + 1)}
            inputMode="numeric"
            autoComplete={i === 0 ? 'one-time-code' : 'off'}
            maxLength={length}
            value={d}
            disabled={disabled}
            // eslint-disable-next-line jsx-a11y/no-autofocus -- the code screen exists only to type the code
            autoFocus={autoFocus && i === 0}
            aria-invalid={invalid || undefined}
            aria-describedby={invalid || help ? msgId : undefined}
            className={cx(invalid && 'tz-input-invalid')}
            onChange={(e) => onInput(i, e.target.value)}
            onKeyDown={(e) => onKeyDown(i, e)}
            onPaste={onPaste}
            onFocus={(e) => e.target.select()}
          />
        ))}
      </div>
      {invalid ? (
        <span className="tz-field-error" id={msgId} role="alert">
          {error}
        </span>
      ) : (
        help && (
          <span className="tz-field-help" id={msgId}>
            {help}
          </span>
        )
      )}
    </div>
  );
}
