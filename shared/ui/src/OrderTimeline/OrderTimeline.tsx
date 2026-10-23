import { Check, X } from 'lucide-react';
import { type ReactNode } from 'react';
import { Icon, type LucideIcon } from '../internal/Icon';
import { VisuallyHidden } from '../internal/VisuallyHidden';
import { cx } from '../internal/cx';

export type StepState = 'done' | 'now' | 'upcoming' | 'failed';

export interface TimelineStep {
  key: string;
  label: ReactNode;
  state: StepState;
  /** Pre-formatted time and note, e.g. "6:18 pm · show your code at the counter". */
  detail?: ReactNode;
  /** Icon in the current step's dot (e.g. ShoppingBag for "Ready for pickup"). */
  icon?: LucideIcon | undefined;
}

export interface OrderTimelineProps {
  /** Accessible name of the list, e.g. "Order progress". */
  label: string;
  steps: readonly TimelineStep[];
  /** Words read after each step's label, e.g. { done: "done", upcoming: "not yet", failed: "failed" }. */
  stateLabels: { done: string; upcoming: string; failed: string };
  className?: string | undefined;
}

/** Vertical order progress (tz-steps). The current step is aria-current="step". */
export function OrderTimeline({ label, steps, stateLabels, className }: OrderTimelineProps) {
  return (
    <ol className={cx('tz-steps', className)} aria-label={label}>
      {steps.map((s) => (
        <li
          key={s.key}
          data-done={s.state === 'done' ? '' : undefined}
          data-now={s.state === 'now' ? '' : undefined}
          data-failed={s.state === 'failed' ? '' : undefined}
          aria-current={s.state === 'now' ? 'step' : undefined}
        >
          <span className="tz-dot">
            {s.state === 'done' && <Icon icon={Check} small />}
            {s.state === 'failed' && <Icon icon={X} small />}
            {s.state === 'now' && s.icon && <Icon icon={s.icon} small />}
          </span>
          <div>
            <b>
              {s.label}
              {s.state !== 'now' && <VisuallyHidden>, {stateLabels[s.state]}</VisuallyHidden>}
            </b>
            {s.detail !== undefined && <small>{s.detail}</small>}
          </div>
        </li>
      ))}
    </ol>
  );
}
