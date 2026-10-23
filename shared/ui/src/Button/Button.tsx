import { LoaderCircle } from 'lucide-react';
import { forwardRef, type ButtonHTMLAttributes, type MouseEvent } from 'react';
import { cx } from '../internal/cx';
import { Icon, type LucideIcon } from '../internal/Icon';

export type ButtonVariant = 'default' | 'primary' | 'brand' | 'ghost' | 'danger' | 'soft' | 'soft-danger';
export type ButtonSize = 'sm' | 'md' | 'lg';

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  /**
   * primary: the main action (near-black). brand: only where money changes hands (place order,
   * charge). danger: destructive. soft / soft-danger / ghost: quiet secondary actions.
   */
  variant?: ButtonVariant | undefined;
  size?: ButtonSize | undefined;
  /** Full width. */
  block?: boolean | undefined;
  /** Leading Lucide icon (16px inside buttons). */
  icon?: LucideIcon | undefined;
  /**
   * Shows a spinner and blocks clicks (and form submission) while keeping focus on the button:
   * aria-disabled and aria-busy, not the disabled attribute. The label stays, so the width holds.
   */
  loading?: boolean | undefined;
  /**
   * Announced politely when loading starts, e.g. "Charging". It also joins the button's name while
   * loading. Without it screen readers only get aria-busy, which most do not read.
   */
  loadingLabel?: string | undefined;
}

/** Button with every design-system variant (tz-btn). Labels are sentence case, verb first. */
export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  {
    variant = 'default',
    size = 'md',
    block = false,
    icon,
    loading = false,
    loadingLabel,
    disabled,
    className,
    children,
    type = 'button',
    onClick,
    ...rest
  },
  ref,
) {
  const busy = loading && disabled !== true;
  const click = (e: MouseEvent<HTMLButtonElement>) => {
    if (busy) {
      // Also cancels the submit of a type=submit button, including Enter in a field.
      e.preventDefault();
      return;
    }
    onClick?.(e);
  };
  return (
    <button
      ref={ref}
      type={type}
      className={cx(
        'tz-btn',
        variant !== 'default' && `tz-btn-${variant}`,
        size !== 'md' && `tz-btn-${size}`,
        block && 'tz-btn-block',
        className,
      )}
      disabled={disabled}
      aria-disabled={busy || undefined}
      aria-busy={loading || undefined}
      {...rest}
      onClick={click}
    >
      {loading ? <Icon icon={LoaderCircle} className="tz-ui-spin" /> : icon && <Icon icon={icon} />}
      {children}
      {loadingLabel !== undefined && (
        // Always present, so the live region exists before the text arrives.
        <span className="tz-ui-sr-only" aria-live="polite">
          {busy ? loadingLabel : ''}
        </span>
      )}
    </button>
  );
});

export interface IconButtonProps extends Omit<ButtonProps, 'children' | 'icon' | 'block' | 'loading' | 'loadingLabel'> {
  icon: LucideIcon;
  /** Accessible name; icon-only buttons always need one. */
  label: string;
}

/** Square outlined icon-only button (tz-btn-icon), never a circle. */
export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(function IconButton(
  { icon, label, className, size = 'md', variant = 'default', type = 'button', ...rest },
  ref,
) {
  return (
    <button
      ref={ref}
      type={type}
      aria-label={label}
      className={cx(
        'tz-btn',
        'tz-btn-icon',
        variant !== 'default' && `tz-btn-${variant}`,
        size === 'sm' && 'tz-btn-sm',
        className,
      )}
      {...rest}
    >
      <Icon icon={icon} />
    </button>
  );
});
