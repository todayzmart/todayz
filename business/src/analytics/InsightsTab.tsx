import type { InsightKind } from '@todayz/api-client/gen/model/insightKind';
import { Button, Chip, EmptyState, INSIGHT_LOOK, Skeleton, canDismissInsight, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { CircleAlert, Inbox, X } from 'lucide-react';
import { useId, useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { actionHref, actionLabel, kindLabel, sentence } from '../overview/insightRows';
import { dismissInsight } from '../overview/queries';
import { RouterLink } from '../overview/RouterLink';
import type { InsightData } from '../overview/types';
import { needsLine } from './needs';
import { useAllInsights } from './queries';
import styles from './analytics.module.css';

interface Group {
  kind: InsightKind;
  rows: InsightData[];
}

/** Consecutive rows of one kind: the API sends them grouped, in the kind order of 01 §12. */
function groupByKind(items: readonly InsightData[]): Group[] {
  const out: Group[] = [];
  for (const i of items) {
    const last = out[out.length - 1];
    if (last && last.kind === i.kind) last.rows.push(i);
    else out.push({ kind: i.kind, rows: [i] });
  }
  return out;
}

/**
 * B-12 Insights tab (D-99): every row of `GET {S}/insights?all=1`, grouped by kind with a kind
 * filter. A kind below its minimum evidence shows a muted line saying what it needs, and no action.
 * Expired on shelf has no "Not useful".
 */
export function InsightsTab({ store, kind }: { store: CurrentStore; kind: InsightKind | undefined }) {
  const intl = useIntl();
  const navigate = useNavigate();
  // The filter lists the kinds of the unfiltered answer, so a kind that is switched off never shows.
  const every = useAllInsights(store, undefined);
  const list = useAllInsights(store, kind);
  const kinds = useMemo(() => {
    const seen: InsightKind[] = [];
    for (const p of every.data?.pages ?? []) for (const i of p.items) if (!seen.includes(i.kind)) seen.push(i.kind);
    if (kind && !seen.includes(kind)) seen.push(kind);
    return seen;
  }, [every.data, kind]);
  const items = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  const [hidden, setHidden] = useState<ReadonlySet<string>>(() => new Set());
  const hide = (id: string) => setHidden((h) => new Set(h).add(id));
  const show = (id: string) =>
    setHidden((h) => {
      const n = new Set(h);
      n.delete(id);
      return n;
    });
  const groups = groupByKind(items.filter((i) => !hidden.has(i.id)));
  const pick = (k: InsightKind | undefined) =>
    void navigate({ to: '/analytics', search: k ? { tab: 'insights', kind: k } : { tab: 'insights' }, replace: true });

  if (list.isError) {
    return (
      <EmptyState
        icon={Inbox}
        title={intl.formatMessage({ id: 'analytics.insights.error' })}
        action={<Button onClick={() => void list.refetch()}>{intl.formatMessage({ id: 'overview.retry' })}</Button>}
      />
    );
  }
  return (
    <div className={styles.stack}>
      <p className={styles.sub}>
        {intl.formatMessage({ id: 'analytics.insights.intro' }, { days: list.data?.pages[0]?.period.days ?? 7 })}
      </p>
      {kinds.length > 0 && (
        <div className={styles.chips} role="group" aria-label={intl.formatMessage({ id: 'analytics.insights.filter' })}>
          <Chip pressed={kind === undefined} onPressedChange={() => pick(undefined)}>
            {intl.formatMessage({ id: 'analytics.insights.allKinds' })}
          </Chip>
          {kinds.map((k) => (
            <Chip key={k} pressed={kind === k} onPressedChange={() => pick(k)}>
              {kindLabel(intl, k)}
            </Chip>
          ))}
        </div>
      )}
      {list.isPending ? (
        <div role="status" aria-label={intl.formatMessage({ id: 'analytics.loading' })} className={styles.stack}>
          <Skeleton shape="block" height="58px" />
          <Skeleton shape="block" height="58px" />
          <Skeleton shape="block" height="58px" />
        </div>
      ) : groups.length === 0 ? (
        <p className={styles.note}>{intl.formatMessage({ id: 'analytics.insights.none' })}</p>
      ) : (
        groups.map((g) => <KindGroup key={g.kind} store={store} group={g} onHide={hide} onShow={show} />)
      )}
      {list.hasNextPage && (
        <div className={styles.more}>
          <Button loading={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
            {intl.formatMessage({ id: 'analytics.more' })}
          </Button>
        </div>
      )}
    </div>
  );
}

interface KindGroupProps {
  store: CurrentStore;
  group: Group;
  onHide: (id: string) => void;
  onShow: (id: string) => void;
}

function KindGroup({ store, group, onHide, onShow }: KindGroupProps) {
  const intl = useIntl();
  const headId = useId();
  const look = INSIGHT_LOOK[group.kind];
  const Icon = look.icon;
  return (
    <section className={`tz-card ${styles.group}`} aria-labelledby={headId}>
      <h2 id={headId} className="tz-card-title">
        <span className={styles.kindIcon} data-tint={look.tint} aria-hidden="true">
          <Icon size={16} />
        </span>
        {kindLabel(intl, group.kind)}
      </h2>
      <ul className={styles.rows}>
        {group.rows.map((d) =>
          d.evidence.state === 'ready' && d.action ? (
            <InsightLine key={d.id} store={store} d={d} onHide={onHide} onShow={onShow} />
          ) : (
            <li key={d.id} className={styles.needs}>
              {needsLine(intl, d)}
            </li>
          ),
        )}
      </ul>
    </section>
  );
}

function InsightLine({ store, d, onHide, onShow }: { store: CurrentStore; d: InsightData; onHide: (id: string) => void; onShow: (id: string) => void }) {
  const intl = useIntl();
  const toast = useToast();
  const qc = useQueryClient();
  const sentenceId = useId();
  const action = d.action;
  if (!action) return null;
  const notUseful = intl.formatMessage({ id: 'overview.insights.notUseful' });
  const dismiss = () => {
    if (!d.subjectRef) return;
    onHide(d.id);
    dismissInsight(qc, store, d.kind, d.subjectRef).then(
      (until) =>
        toast.show({
          message: intl.formatMessage(
            { id: 'overview.insights.hiddenUntil' },
            { date: intl.formatDate(new Date(until), { day: 'numeric', month: 'short', timeZone: 'Asia/Kolkata' }) },
          ),
        }),
      () => {
        onShow(d.id);
        toast.show({ icon: CircleAlert, message: intl.formatMessage({ id: 'overview.insights.dismissFailed' }) });
      },
    );
  };
  return (
    <li className={styles.row}>
      <span id={sentenceId} className={styles.sentence}>
        {sentence(intl, d)}
      </span>
      <span className={styles.rowActions}>
        <RouterLink href={actionHref(action)} aria-describedby={sentenceId}>
          {actionLabel(intl, action)}
          <span aria-hidden="true"> →</span>
        </RouterLink>
        {canDismissInsight(d.kind) && d.subjectRef && (
          <button
            type="button"
            className="tz-ui-insight-dismiss"
            aria-label={notUseful}
            aria-describedby={sentenceId}
            title={notUseful}
            onClick={dismiss}
          >
            <X size={14} aria-hidden="true" />
          </button>
        )}
      </span>
    </li>
  );
}
