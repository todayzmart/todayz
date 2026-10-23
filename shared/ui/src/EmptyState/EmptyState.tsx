import { Inbox } from 'lucide-react';
import { type ReactNode } from 'react';
import { cx } from '../internal/cx';
import { Icon, type LucideIcon } from '../internal/Icon';

export interface EmptyStateProps {
  title: ReactNode;
  description?: ReactNode;
  icon?: LucideIcon | undefined;
  /** One primary next step, e.g. a Button. */
  action?: ReactNode;
  /** Extra content such as a setup checklist. */
  children?: ReactNode;
  /** Heading level for the title inside the page outline. */
  headingLevel?: 2 | 3 | 4 | undefined;
  className?: string | undefined;
}

/** Empty state: what is missing and the one thing to do next. */
export function EmptyState({
  title,
  description,
  icon = Inbox,
  action,
  children,
  headingLevel = 3,
  className,
}: EmptyStateProps) {
  const H = `h${headingLevel}` as const;
  return (
    <div className={cx('tz-ui-empty', className)}>
      <span className="tz-iconring">
        <Icon icon={icon} />
      </span>
      <H className="tz-ui-empty-title">{title}</H>
      {description !== undefined && <p className="tz-ui-empty-desc">{description}</p>}
      {children}
      {action !== undefined && <div className="tz-ui-empty-action">{action}</div>}
    </div>
  );
}
