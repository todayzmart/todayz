import { User } from 'lucide-react';
import { type ReactNode } from 'react';
import { Button } from '../Button/Button';
import { ChipGroup } from '../Chip/Chip';
import { cx } from '../internal/cx';
import { Icon } from '../internal/Icon';
import { QuantityStepper, type QuantityStepperLabels } from '../QuantityStepper/QuantityStepper';

export interface PosLine {
  id: string;
  name: string;
  /** Pre-formatted unit and unit price, e.g. "5 kg · ₹289". */
  detail?: ReactNode;
  quantity: number;
  /** Pre-formatted line total, e.g. "₹578". */
  total: ReactNode;
  /** Stock on hand, if the counter should stop there. */
  max?: number | undefined;
}

export interface PosCartProps {
  /** Accessible name of the bill, e.g. "Current bill". */
  label: string;
  customer: {
    /** e.g. "Customer phone". */
    label: string;
    placeholder?: string | undefined;
    phone: string;
    onPhoneChange: (phone: string) => void;
    /** Who the number belongs to, e.g. "Ananya R. · 6 visits · 420 points". */
    summary?: ReactNode;
  };
  lines: readonly PosLine[];
  /** Quantity 0 removes the line; the app decides. */
  onQuantityChange: (id: string, quantity: number) => void;
  stepperLabels: QuantityStepperLabels;
  /** Shown instead of the lines when the bill is empty. */
  emptyLabel: ReactNode;
  payment: {
    /** e.g. "Payment method". */
    label: string;
    options: readonly { value: string; label: ReactNode }[];
    value: string;
    onChange: (value: string) => void;
  };
  /** e.g. "11 items · ₹5 discount". */
  summary?: ReactNode;
  /** Pre-formatted amount due, e.g. "₹1,236". */
  amount: ReactNode;
  chargeLabel: string;
  onCharge: () => void;
  /** Charging in progress: the button spins and blocks double charges, and keeps focus. */
  charging?: boolean | undefined;
  /** Announced when charging starts, e.g. "Charging". */
  chargingLabel?: string | undefined;
  className?: string | undefined;
}

/**
 * The counter bill (POS): customer lookup, lines with steppers, payment method and the inverse
 * total bar with the brand Charge button. Charge is disabled on an empty bill.
 */
export function PosCart({
  label,
  customer,
  lines,
  onQuantityChange,
  stepperLabels,
  emptyLabel,
  payment,
  summary,
  amount,
  chargeLabel,
  chargingLabel,
  onCharge,
  charging = false,
  className,
}: PosCartProps) {
  return (
    <section className={cx('tz-ui-poscart', className)} aria-label={label}>
      <div className="tz-search tz-ui-poscart-phone">
        <Icon icon={User} />
        <input
          type="tel"
          inputMode="tel"
          autoComplete="off"
          aria-label={customer.label}
          placeholder={customer.placeholder}
          value={customer.phone}
          onChange={(e) => customer.onPhoneChange(e.target.value)}
        />
      </div>
      {customer.summary !== undefined && (
        <div className="tz-ui-poscart-customer" aria-live="polite">
          {customer.summary}
        </div>
      )}
      {lines.length === 0 ? (
        <p className="tz-ui-poscart-empty">{emptyLabel}</p>
      ) : (
        <div role="list">
          {lines.map((l) => (
            <div className="tz-pos-line" role="listitem" key={l.id}>
              <div>
                {l.name}
                {l.detail !== undefined && <span className="tz-sub">{l.detail}</span>}
              </div>
              <QuantityStepper
                size="sm"
                alwaysStepper
                quantity={l.quantity}
                max={l.max}
                itemName={l.name}
                labels={stepperLabels}
                onChange={(q) => onQuantityChange(l.id, q)}
              />
              <b className="tz-num">{l.total}</b>
            </div>
          ))}
        </div>
      )}
      <ChipGroup label={payment.label} options={payment.options} value={payment.value} onChange={payment.onChange} />
      <div className="tz-pos-total">
        <div>
          {summary !== undefined && <div className="tz-ui-pos-summary">{summary}</div>}
          <div className="tz-amt" aria-live="polite" aria-atomic="true">
            {amount}
          </div>
        </div>
        <Button
          variant="brand"
          size="lg"
          loading={charging}
          loadingLabel={chargingLabel}
          disabled={lines.length === 0}
          onClick={onCharge}
        >
          {chargeLabel}
        </Button>
      </div>
    </section>
  );
}
