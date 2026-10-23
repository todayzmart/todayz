import { forwardRef, type ButtonHTMLAttributes, type KeyboardEvent, type ReactNode } from 'react';
import { cx } from '../internal/cx';
import { Icon, type LucideIcon } from '../internal/Icon';

export interface ChipProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'onChange'> {
  pressed: boolean;
  onPressedChange?: (pressed: boolean) => void;
  icon?: LucideIcon | undefined;
  children: ReactNode;
}

/** Toggle chip (tz-chip with aria-pressed): filters and single-choice rows. */
export const Chip = forwardRef<HTMLButtonElement, ChipProps>(function Chip(
  { pressed, onPressedChange, icon, className, children, onClick, ...rest },
  ref,
) {
  return (
    <button
      ref={ref}
      type="button"
      className={cx('tz-chip', className)}
      aria-pressed={pressed}
      onClick={(e) => {
        onClick?.(e);
        if (!e.defaultPrevented) onPressedChange?.(!pressed);
      }}
      {...rest}
    >
      {icon && <Icon icon={icon} small />}
      {children}
    </button>
  );
});

export interface ChipOption<V extends string | number> {
  value: V;
  label: ReactNode;
  icon?: LucideIcon | undefined;
  disabled?: boolean | undefined;
}

interface ChipGroupBase<V extends string | number> {
  /** Accessible name of the group. */
  label: string;
  options: readonly ChipOption<V>[];
  className?: string | undefined;
}

export interface SingleChipGroupProps<V extends string | number> extends ChipGroupBase<V> {
  multiple?: false;
  value: V;
  onChange: (value: V) => void;
}

export interface MultiChipGroupProps<V extends string | number> extends ChipGroupBase<V> {
  multiple: true;
  value: readonly V[];
  onChange: (value: V[]) => void;
}

export type ChipGroupProps<V extends string | number> = SingleChipGroupProps<V> | MultiChipGroupProps<V>;

/**
 * A row of chips (tz-chips). Single choice behaves like a radio row: pressing the pressed chip
 * keeps it, and Left/Right arrows move between chips. Multiple choice toggles each chip.
 */
export function ChipGroup<V extends string | number>(props: ChipGroupProps<V>) {
  const { label, options, className } = props;
  const isPressed = (v: V) => (props.multiple ? props.value.includes(v) : props.value === v);
  const press = (v: V) => {
    if (props.multiple) {
      props.onChange(props.value.includes(v) ? props.value.filter((x) => x !== v) : [...props.value, v]);
    } else if (props.value !== v) {
      props.onChange(v);
    }
  };
  const onKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return;
    const row = e.currentTarget.parentElement;
    if (!row) return;
    const buttons = [...row.querySelectorAll<HTMLButtonElement>(':scope > button:not([disabled])')];
    const at = buttons.indexOf(e.currentTarget);
    if (at < 0) return;
    e.preventDefault();
    const next = buttons[(at + (e.key === 'ArrowRight' ? 1 : buttons.length - 1)) % buttons.length];
    next?.focus();
  };
  return (
    <div className={cx('tz-chips', className)} role="group" aria-label={label}>
      {options.map((o) => (
        <Chip
          key={String(o.value)}
          pressed={isPressed(o.value)}
          onPressedChange={() => press(o.value)}
          icon={o.icon}
          disabled={o.disabled}
          onKeyDown={onKeyDown}
        >
          {o.label}
        </Chip>
      ))}
    </div>
  );
}

export interface RadiusChipsProps {
  /** Accessible name, e.g. "Search radius". */
  label: string;
  /** Radius options in km from platform settings (01-product-rules), each with its label ("2 km"). */
  options: readonly { km: number; label: string }[];
  value: number;
  onChange: (km: number) => void;
  className?: string | undefined;
}

/** Single-choice search radius chips (1, 2, 5, 10, 20 km by default in the product rules). */
export function RadiusChips({ label, options, value, onChange, className }: RadiusChipsProps) {
  return (
    <ChipGroup
      label={label}
      className={className}
      options={options.map((o) => ({ value: o.km, label: o.label }))}
      value={value}
      onChange={onChange}
    />
  );
}
