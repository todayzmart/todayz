import { X } from 'lucide-react';
import { useId, type ReactNode } from 'react';
import { cx } from '../internal/cx';
import { Icon } from '../internal/Icon';
import type { LinkComponent } from '../internal/link';
import { Skeleton } from '../Skeleton/Skeleton';
import { canDismissInsight, INSIGHT_LOOK, pickInsightRows, type InsightRow } from './insights';

export interface DemandInsightsProps {
  title: ReactNode;
  description?: ReactNode;
  /** e.g. "Last 7 days" (follows insight_period_days). */
  periodTag?: ReactNode;
  /** Rows already ordered by severity by the API. The card keeps the first per kind, up to `limit`. */
  rows: readonly InsightRow[];
  /** Card row limit (01 §17: 4). */
  limit?: number | undefined;
  /** "Not useful": quiets that row's subject. Never offered for expired_on_shelf. */
  onNotUseful?: ((row: InsightRow) => void) | undefined;
  notUsefulLabel?: string | undefined;
  /** "All insights" link to the Insights tab. */
  allLink?: { label: ReactNode; href: string } | undefined;
  footNote?: ReactNode;
  /** No kind fired: "Nothing needs you right now" plus the setup checklist. */
  empty: { title: ReactNode; checklist?: ReactNode };
  loading?: boolean | undefined;
  LinkComponent?: LinkComponent | undefined;
  className?: string | undefined;
}

function Row({
  row,
  onNotUseful,
  notUsefulLabel,
  LinkComponent,
}: { row: InsightRow } & Pick<DemandInsightsProps, 'onNotUseful' | 'notUsefulLabel' | 'LinkComponent'>) {
  const sentenceId = useId();
  const look = INSIGHT_LOOK[row.kind];
  const Link = LinkComponent ?? 'a';
  const action = (
    <Link href={row.action.href} aria-describedby={sentenceId}>
      {row.action.label}
      <span aria-hidden="true"> →</span>
    </Link>
  );
  const dismissable = onNotUseful !== undefined && notUsefulLabel !== undefined && canDismissInsight(row.kind);
  return (
    <div className="tz-insight" data-tint={look.tint} data-kind={row.kind}>
      <span className="tz-insight-icon">
        <Icon icon={look.icon} />
      </span>
      <div>
        <b>{row.label}</b>
        <small id={sentenceId}>{row.sentence}</small>
      </div>
      {dismissable ? (
        <div className="tz-ui-insight-actions">
          {action}
          <button
            type="button"
            className="tz-ui-insight-dismiss"
            aria-label={notUsefulLabel}
            title={notUsefulLabel}
            onClick={() => onNotUseful(row)}
          >
            <Icon icon={X} small />
          </button>
        </div>
      ) : (
        action
      )}
    </div>
  );
}

/** The B-03 "Demand insights" card (D-99): up to 4 tinted rows, each with one text action. */
export function DemandInsights({
  title,
  description,
  periodTag,
  rows,
  limit = 4,
  onNotUseful,
  notUsefulLabel,
  allLink,
  footNote,
  empty,
  loading = false,
  LinkComponent,
  className,
}: DemandInsightsProps) {
  const headingId = useId();
  const shown = pickInsightRows(rows, limit);
  const Link = LinkComponent ?? 'a';
  return (
    <section className={cx('tz-card', className)} aria-labelledby={headingId} aria-busy={loading || undefined}>
      <div className="tz-card-head">
        <div>
          <h2 className="tz-card-title" id={headingId}>
            {title}
          </h2>
          {description !== undefined && <p className="tz-card-desc">{description}</p>}
        </div>
        {periodTag !== undefined && <span className="tz-tag">{periodTag}</span>}
      </div>
      {loading ? (
        <div className="tz-insights">
          {Array.from({ length: limit }, (_, i) => (
            <Skeleton key={i} shape="block" height="58px" />
          ))}
        </div>
      ) : shown.length === 0 ? (
        <div className="tz-ui-insights-empty">
          <b>{empty.title}</b>
          {empty.checklist}
        </div>
      ) : (
        <div className="tz-insights">
          {shown.map((r) => (
            <Row
              key={r.id}
              row={r}
              onNotUseful={onNotUseful}
              notUsefulLabel={notUsefulLabel}
              LinkComponent={LinkComponent}
            />
          ))}
        </div>
      )}
      {(allLink || footNote !== undefined) && (
        <div className="tz-card-foot">
          {allLink && <Link href={allLink.href}>{allLink.label}</Link>}
          {footNote !== undefined && <span>{footNote}</span>}
        </div>
      )}
    </section>
  );
}
