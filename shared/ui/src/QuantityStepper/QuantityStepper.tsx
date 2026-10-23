import { Minus, Plus } from 'lucide-react';
import { useEffect, useRef } from 'react';
import { cx } from '../internal/cx';
import { Icon } from '../internal/Icon';
import { VisuallyHidden } from '../internal/VisuallyHidden';

export interface QuantityStepperLabels {
  /** The pill shown at 0, e.g. "Add". */
  add: string;
  /** e.g. "Remove one". */
  decrement: string;
  /** e.g. "Add one". */
  increment: string;
  /**
   * Read politely with the count when it reaches `max`, e.g. (3) => "Only 3 in stock". Without it
   * only the count is read and + is announced as dimmed.
   */
  max?: ((max: number) => string) | undefined;
}

export interface QuantityStepperProps {
  quantity: number;
  onChange: (quantity: number) => void;
  /** Most the customer may take (stock on hand); + is dimmed there (aria-disabled, keeps focus). */
  max?: number | undefined;
  labels: QuantityStepperLabels;
  /**
   * Name of the item, read with the buttons ("Add one, Amul Taaza Milk") when several
   * steppers share a screen.
   */
  itemName?: string | undefined;
  /** Hide the Add pill and always show the stepper (POS lines). */
  alwaysStepper?: boolean | undefined;
  disabled?: boolean | undefined;
  /** sm: 32px buttons for dense lists such as the POS bill. */
  size?: 'md' | 'sm' | undefined;
  className?: string | undefined;
}

/**
 * Add pill that turns into a − n + stepper in place (tz-add / tz-stepper). The count is announced
 * politely; focus follows the control when it swaps so keyboard users never lose their place.
 */
export function QuantityStepper({
  quantity,
  onChange,
  max,
  labels,
  itemName,
  alwaysStepper = false,
  disabled = false,
  size = 'md',
  className,
}: QuantityStepperProps) {
  const addRef = useRef<HTMLButtonElement>(null);
  const incRef = useRef<HTMLButtonElement>(null);
  const prev = useRef(quantity);
  /** The quantity this control last asked for; cleared on the next quantity change. */
  const requested = useRef<number | null>(null);

  useEffect(() => {
    const from = prev.current;
    const asked = requested.current;
    prev.current = quantity;
    requested.current = null;
    // Follow the swap (Add pill to stepper and back) only for this control's own change, and only
    // when the swap dropped focus: a change the parent made later, or after the user moved on,
    // keeps focus where it is.
    if (asked !== quantity) return;
    const active = document.activeElement;
    if (active && active !== document.body) return;
    if (from === 0 && quantity > 0) incRef.current?.focus();
    else if (from > 0 && quantity === 0) addRef.current?.focus();
  }, [quantity]);

  const change = (next: number) => {
    requested.current = next;
    onChange(next);
  };
  const named = (l: string) => (itemName ? `${l}, ${itemName}` : l);
  const atMax = max !== undefined && quantity >= max;
  const atZero = quantity <= 0;

  if (quantity <= 0 && !alwaysStepper) {
    return (
      <button
        ref={addRef}
        type="button"
        className={cx('tz-add', className)}
        aria-label={itemName ? named(labels.add) : undefined}
        disabled={disabled || (max !== undefined && max <= 0)}
        onClick={() => change(1)}
      >
        {labels.add}
      </button>
    );
  }
  // At a limit the button stays focusable (aria-disabled) so focus is not dropped mid-tap.
  return (
    <div className={cx('tz-stepper', size === 'sm' && 'tz-ui-stepper-sm', className)}>
      <button
        type="button"
        aria-label={named(labels.decrement)}
        disabled={disabled}
        aria-disabled={(!disabled && atZero) || undefined}
        onClick={() => {
          if (!atZero) change(quantity - 1);
        }}
      >
        <Icon icon={Minus} small />
      </button>
      <span aria-live="polite" aria-atomic="true">
        {quantity}
        {atMax && labels.max && <VisuallyHidden>{labels.max(max)}</VisuallyHidden>}
      </span>
      <button
        ref={incRef}
        type="button"
        aria-label={named(labels.increment)}
        disabled={disabled}
        aria-disabled={(!disabled && atMax) || undefined}
        onClick={() => {
          if (!atMax) change(quantity + 1);
        }}
      >
        <Icon icon={Plus} small />
      </button>
    </div>
  );
}
