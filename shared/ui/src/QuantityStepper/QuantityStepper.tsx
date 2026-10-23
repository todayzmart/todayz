import { Minus, Plus } from 'lucide-react';
import { useEffect, useRef } from 'react';
import { cx } from '../internal/cx';
import { Icon } from '../internal/Icon';

export interface QuantityStepperLabels {
  /** The pill shown at 0, e.g. "Add". */
  add: string;
  /** e.g. "Remove one". */
  decrement: string;
  /** e.g. "Add one". */
  increment: string;
}

export interface QuantityStepperProps {
  quantity: number;
  onChange: (quantity: number) => void;
  /** Most the customer may take (stock on hand); + is disabled there. */
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
  const userAction = useRef(false);

  useEffect(() => {
    if (userAction.current) {
      if (prev.current === 0 && quantity > 0) incRef.current?.focus();
      if (prev.current > 0 && quantity === 0) addRef.current?.focus();
    }
    userAction.current = false;
    prev.current = quantity;
  }, [quantity]);

  const change = (next: number) => {
    userAction.current = true;
    onChange(next);
  };
  const named = (l: string) => (itemName ? `${l}, ${itemName}` : l);
  const atMax = max !== undefined && quantity >= max;

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
  return (
    <div className={cx('tz-stepper', size === 'sm' && 'tz-ui-stepper-sm', className)}>
      <button
        type="button"
        aria-label={named(labels.decrement)}
        disabled={disabled || quantity <= 0}
        onClick={() => change(quantity - 1)}
      >
        <Icon icon={Minus} small />
      </button>
      <span aria-live="polite" aria-atomic="true">
        {quantity}
      </span>
      <button
        ref={incRef}
        type="button"
        aria-label={named(labels.increment)}
        disabled={disabled || atMax}
        onClick={() => change(quantity + 1)}
      >
        <Icon icon={Plus} small />
      </button>
    </div>
  );
}
