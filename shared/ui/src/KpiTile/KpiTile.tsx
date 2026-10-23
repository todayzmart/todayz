import { type ReactNode } from 'react';
import { cx } from '../internal/cx';
import { Icon, type LucideIcon } from '../internal/Icon';
import type { LinkComponent } from '../internal/link';
import { VisuallyHidden } from '../internal/VisuallyHidden';
import { Skeleton } from '../Skeleton/Skeleton';

export interface KpiDelta {
  /** Pre-formatted change, e.g. "12%" or "5". */
  value: ReactNode;
  direction: 'up' | 'down' | 'flat';
  /** What screen readers hear instead of the arrow, e.g. "Up 12%". */
  label: string;
}

export interface KpiProps {
  name: ReactNode;
  icon?: LucideIcon | undefined;
  /** Pre-formatted value, e.g. "₹48,260". */
  value: ReactNode;
  /** One short line under the value, e.g. "142 bills · 37 online orders". */
  note?: ReactNode;
  delta?: KpiDelta | undefined;
  /** What the delta compares with, e.g. "vs last Thursday". */
  comparison?: ReactNode;
  /** A foot link instead of the delta, e.g. "Reorder list". */
  link?: { label: ReactNode; href: string } | undefined;
  loading?: boolean | undefined;
}

const ARROW = { up: '▲', down: '▼', flat: '–' } as const;

function Delta({ delta }: { delta: KpiDelta }) {
  return (
    <span
      className={cx(
        'tz-kpi-delta',
        delta.direction === 'down' && 'tz-kpi-delta-down',
        delta.direction === 'flat' && 'tz-kpi-delta-flat',
      )}
    >
      <span aria-hidden="true">
        {ARROW[delta.direction]} {delta.value}
      </span>
      <VisuallyHidden>{delta.label}</VisuallyHidden>
    </span>
  );
}

function FootLink({
  link,
  LinkComponent,
}: {
  link: NonNullable<KpiProps['link']>;
  LinkComponent?: LinkComponent | undefined;
}) {
  const Link = LinkComponent ?? 'a';
  return (
    <Link href={link.href}>
      {link.label}
      <span aria-hidden="true"> →</span>
    </Link>
  );
}

export interface KpiTileProps extends KpiProps {
  LinkComponent?: LinkComponent | undefined;
  className?: string | undefined;
}

/** A single KPI card (tz-kpi), for grids where the strip does not fit. */
export function KpiTile({
  name,
  icon,
  value,
  note,
  delta,
  comparison,
  link,
  loading = false,
  LinkComponent,
  className,
}: KpiTileProps) {
  return (
    <div className={cx('tz-kpi', className)} aria-busy={loading || undefined}>
      <span className="tz-kpi-label">
        {icon && <Icon icon={icon} />}
        {name}
      </span>
      <span className="tz-kpi-value">{loading ? <Skeleton width="6ch" height="1em" /> : value}</span>
      {note !== undefined && <span className="tz-kpi-note">{note}</span>}
      {(delta || comparison !== undefined || link) && (
        <div className="tz-kpi-foot">
          {link ? (
            <FootLink link={link} LinkComponent={LinkComponent} />
          ) : (
            <>
              {delta && <Delta delta={delta} />}
              {comparison}
            </>
          )}
        </div>
      )}
    </div>
  );
}

export interface KpiStripProps {
  /** Accessible name, e.g. "Today at a glance". */
  label: string;
  /** The first item is the lead cell (wash, larger value). Four fit one row; two per row below 720px. */
  items: readonly (KpiProps & { key: string })[];
  LinkComponent?: LinkComponent | undefined;
  className?: string | undefined;
}

/** One-row KPI strip with hairline dividers (tz-kpi-strip); the first cell leads. */
export function KpiStrip({ label, items, LinkComponent, className }: KpiStripProps) {
  return (
    <section className={cx('tz-kpi-strip', className)} aria-label={label}>
      {items.map(({ key, name, icon, value, note, delta, comparison, link, loading }, i) => (
        <div key={key} className={cx('tz-kpi-cell', i === 0 && 'tz-kpi-cell-lead')} aria-busy={loading || undefined}>
          <div className="tz-kpi-cell-body">
            <span className="tz-kpi-name">
              {icon && <Icon icon={icon} />}
              {name}
            </span>
            <span className="tz-kpi-value">{loading ? <Skeleton width="6ch" height="1em" /> : value}</span>
            {note !== undefined && <span className="tz-kpi-note">{note}</span>}
          </div>
          {(delta || comparison !== undefined || link) && (
            <div className="tz-kpi-cell-foot">
              {link ? (
                <FootLink link={link} LinkComponent={LinkComponent} />
              ) : (
                <>
                  {delta && <Delta delta={delta} />}
                  {comparison}
                </>
              )}
            </div>
          )}
        </div>
      ))}
    </section>
  );
}
