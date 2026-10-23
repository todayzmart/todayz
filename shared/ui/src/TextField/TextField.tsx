import { forwardRef, useId, type InputHTMLAttributes, type ReactNode } from 'react';
import { cx } from '../internal/cx';

export interface TextFieldProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'prefix'> {
  label: ReactNode;
  /** One line under the field. Hidden while an error shows. */
  help?: ReactNode;
  /** Error text; marks the input invalid and is announced with it. */
  error?: ReactNode;
  /** Fixed text before the input, e.g. "+91" for phone numbers. */
  prefix?: ReactNode;
  /** Classes for the outer tz-field. */
  fieldClassName?: string | undefined;
}

/** Labelled input with help and error text (tz-field / tz-input). */
export const TextField = forwardRef<HTMLInputElement, TextFieldProps>(function TextField(
  { label, help, error, prefix, id, className, fieldClassName, type = 'text', ...rest },
  ref,
) {
  const auto = useId();
  const inputId = id ?? `tz-field-${auto}`;
  const helpId = `${inputId}-help`;
  const errorId = `${inputId}-error`;
  const invalid = error !== undefined && error !== null && error !== false;
  const describedBy = invalid ? errorId : help ? helpId : undefined;
  const input = (
    <input
      ref={ref}
      id={inputId}
      type={type}
      className={cx('tz-input', invalid && 'tz-input-invalid', className)}
      aria-invalid={invalid || undefined}
      aria-describedby={describedBy}
      {...rest}
    />
  );
  return (
    <div className={cx('tz-field', fieldClassName)}>
      <label className="tz-field-label" htmlFor={inputId}>
        {label}
      </label>
      {prefix ? (
        <div className={cx('tz-ui-affix', invalid && 'tz-ui-affix-invalid')}>
          <span className="tz-ui-affix-text">{prefix}</span>
          {input}
        </div>
      ) : (
        input
      )}
      {invalid ? (
        <span className="tz-field-error" id={errorId}>
          {error}
        </span>
      ) : (
        help && (
          <span className="tz-field-help" id={helpId}>
            {help}
          </span>
        )
      )}
    </div>
  );
});
