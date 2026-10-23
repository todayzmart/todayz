import { type ReactNode } from 'react';
import { cx } from '../internal/cx';
import { Icon, type LucideIcon } from '../internal/Icon';

export interface SegmentedOption<V extends string> {
  value: V;
  label: ReactNode;
  icon?: LucideIcon | undefined;
}

export interface SegmentedProps<V extends string> {
  /** Accessible name of the group (e.g. "Period"). */
  label: string;
  options: readonly SegmentedOption<V>[];
  value: V;
  onChange: (value: V) => void;
  className?: string | undefined;
}

/** Segmented control (tz-seg): a small single-choice switch, e.g. Today / 7 days / 30 days. */
export function Segmented<V extends string>({ label, options, value, onChange, className }: SegmentedProps<V>) {
  return (
    <div className={cx('tz-seg', className)} role="group" aria-label={label}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-pressed={o.value === value}
          onClick={() => o.value !== value && onChange(o.value)}
        >
          {o.icon && <Icon icon={o.icon} small />}
          {o.label}
        </button>
      ))}
    </div>
  );
}
