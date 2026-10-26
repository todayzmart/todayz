// A chip that opens the platform's own picker (D-249): Home's and Search's radius, Search's sort.
// The native select keeps the keyboard, screen reader and phone picker; the chip around it draws the look.
import type { LucideIcon } from 'lucide-react';
import { ChevronDown } from 'lucide-react';
import { Icon } from './Icon';
import styles from './chipSelect.module.css';

export interface ChipSelectProps<T extends string | number> {
  /** Accessible name of the select; not shown. */
  label: string;
  value: T;
  options: readonly { value: T; label: string }[];
  onChange: (value: T) => void;
  icon?: LucideIcon | undefined;
}

export function ChipSelect<T extends string | number>({ label, value, options, onChange, icon }: ChipSelectProps<T>) {
  return (
    <label className={`tz-chip ${styles.chip}`} data-icon={icon ? '' : undefined}>
      <span className="tz-ui-sr-only">{label}</span>
      {icon && <Icon icon={icon} />}
      <select
        value={String(value)}
        onChange={(e) => {
          const hit = options.find((o) => String(o.value) === e.target.value);
          if (hit) onChange(hit.value);
        }}
      >
        {options.map((o) => (
          <option key={String(o.value)} value={String(o.value)}>
            {o.label}
          </option>
        ))}
      </select>
      <Icon icon={ChevronDown} />
    </label>
  );
}
