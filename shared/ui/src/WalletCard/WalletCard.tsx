import { useId, type ReactNode } from 'react';
import { cx } from '../internal/cx';

export interface WalletCardProps {
  /** e.g. "TodayZ points". */
  label: ReactNode;
  /** Pre-formatted balance, e.g. "420". */
  points: ReactNode;
  /** e.g. "Worth ₹42 off your next order at any TodayZ shop". */
  worth?: ReactNode;
  /** Buttons such as "Invite and earn 50" (use `<Button size="sm">`). */
  actions?: ReactNode;
  className?: string | undefined;
}

/** Points balance on the inverse card (tz-wallet). */
export function WalletCard({ label, points, worth, actions, className }: WalletCardProps) {
  const id = useId();
  return (
    <section className={cx('tz-wallet', className)} aria-labelledby={id}>
      <small id={id}>{label}</small>
      <span className="tz-wallet-pts">{points}</span>
      {worth !== undefined && <small>{worth}</small>}
      {actions !== undefined && <div className="tz-ui-wallet-actions">{actions}</div>}
    </section>
  );
}
