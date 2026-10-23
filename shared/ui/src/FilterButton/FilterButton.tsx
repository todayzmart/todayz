import { CirclePlus } from 'lucide-react';
import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from 'react';
import { cx } from '../internal/cx';
import { Icon, type LucideIcon } from '../internal/Icon';

export interface FilterButtonProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'value'> {
  /** Filter name, e.g. "Status". */
  label: ReactNode;
  /** Current value, e.g. "Pending". Unset filters are dashed; set ones solid with the value in bold. */
  value?: ReactNode;
  icon?: LucideIcon | undefined;
}

/** Table filter chip that shows its value (tz-filter). Opens a menu or sheet owned by the app. */
export const FilterButton = forwardRef<HTMLButtonElement, FilterButtonProps>(function FilterButton(
  { label, value, icon = CirclePlus, className, type = 'button', ...rest },
  ref,
) {
  const set = value !== undefined && value !== null && value !== '';
  return (
    <button
      ref={ref}
      type={type}
      className={cx('tz-filter', className)}
      data-set={set ? '' : undefined}
      aria-haspopup="true"
      {...rest}
    >
      <Icon icon={icon} small />
      {label}
      {set && (
        <>
          <span aria-hidden="true">·</span>
          <b>{value}</b>
        </>
      )}
    </button>
  );
});
