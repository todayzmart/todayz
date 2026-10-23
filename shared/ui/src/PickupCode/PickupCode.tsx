import { type ReactNode } from 'react';
import { cx } from '../internal/cx';
import { VisuallyHidden } from '../internal/VisuallyHidden';

export interface PickupCodeProps {
  /** e.g. "Pickup code". */
  label: ReactNode;
  /** The customer's 4-digit code. Shown only to that customer; the shop never receives it (D-04). */
  code: string;
  /** e.g. "Show this at Sri Lakshmi Provision Stores". */
  note?: ReactNode;
  /** Extra content under the note, e.g. a hold timer. */
  children?: ReactNode;
  className?: string | undefined;
}

/**
 * Customer-side pickup code (tz-code): big digits on the inverse card. Screen readers hear the
 * digits one by one ("4 8 2 7"), not as a number. The shop side is `OtpInput`.
 */
export function PickupCode({ label, code, note, children, className }: PickupCodeProps) {
  return (
    <div className={cx('tz-code', className)} role="group" aria-label={typeof label === 'string' ? label : undefined}>
      <span className="tz-ui-code-label">{label}</span>
      <span className="tz-code-digits">
        <span aria-hidden="true">{code}</span>
        <VisuallyHidden>{code.split('').join(' ')}</VisuallyHidden>
      </span>
      {note !== undefined && <span className="tz-ui-code-note">{note}</span>}
      {children}
    </div>
  );
}
