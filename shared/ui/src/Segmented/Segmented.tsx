import { useRef, type KeyboardEvent, type ReactNode } from 'react';
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

/**
 * Segmented control (tz-seg): a small single-choice switch, e.g. Today / 7 days / 30 days. A radio
 * group: one tab stop on the chosen option, arrows (and Home/End) move and choose.
 */
export function Segmented<V extends string>({ label, options, value, onChange, className }: SegmentedProps<V>) {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const selected = options.findIndex((o) => o.value === value);
  // With no match the first option takes the tab stop, so the group stays reachable.
  const tabStop = selected >= 0 ? selected : 0;

  const choose = (i: number) => {
    const o = options[i];
    if (!o) return;
    refs.current[i]?.focus();
    if (o.value !== value) onChange(o.value);
  };

  const onKeyDown = (i: number, e: KeyboardEvent<HTMLButtonElement>) => {
    const last = options.length - 1;
    const next =
      e.key === 'ArrowRight' || e.key === 'ArrowDown'
        ? i === last
          ? 0
          : i + 1
        : e.key === 'ArrowLeft' || e.key === 'ArrowUp'
          ? i === 0
            ? last
            : i - 1
          : e.key === 'Home'
            ? 0
            : e.key === 'End'
              ? last
              : -1;
    if (next < 0) return;
    e.preventDefault();
    choose(next);
  };

  return (
    <div className={cx('tz-seg', className)} role="radiogroup" aria-label={label}>
      {options.map((o, i) => (
        <button
          key={o.value}
          ref={(el) => {
            refs.current[i] = el;
          }}
          type="button"
          role="radio"
          aria-checked={o.value === value}
          tabIndex={i === tabStop ? 0 : -1}
          onClick={() => choose(i)}
          onKeyDown={(e) => onKeyDown(i, e)}
        >
          {o.icon && <Icon icon={o.icon} small />}
          {o.label}
        </button>
      ))}
    </div>
  );
}
