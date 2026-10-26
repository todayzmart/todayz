import type { CoachCardFilter } from '@todayz/api-client/gen/model/coachCardFilter';
import { Banner, Button, EmptyState, Skeleton, Tabs, ToastProvider } from '@todayz/ui';
import { Lightbulb } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { StoreGate } from '../overview/StoreGate';
import { CoachCardRow } from './CoachCardRow';
import { useCoachFeed } from './queries';
import styles from './coach.module.css';

type Tab = Extract<CoachCardFilter, 'live' | 'done' | 'dismissed'>;
const TABS: readonly Tab[] = ['live', 'done', 'dismissed'];

/** B-19 Shop coach: the live cards, and what was done or marked not useful. Owner and manager. */
export function CoachPage() {
  return (
    <ToastProvider>
      <StoreGate next="/coach">{(store) => <CoachDesk store={store} />}</StoreGate>
    </ToastProvider>
  );
}

function CoachDesk({ store }: { store: CurrentStore }) {
  const intl = useIntl();
  const [tab, setTab] = useState<Tab>('live');
  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'coach.title' })}</h1>
      <p className={styles.sub}>{intl.formatMessage({ id: 'coach.desc' })}</p>
      <Tabs
        label={intl.formatMessage({ id: 'coach.tabs' })}
        value={tab}
        onChange={setTab}
        tabs={TABS.map((t) => ({ value: t, label: intl.formatMessage({ id: `coach.tab.${t}` }) }))}
      />
      <Feed key={tab} store={store} status={tab} />
    </div>
  );
}

function Feed({ store, status }: { store: CurrentStore; status: Tab }) {
  const intl = useIntl();
  const feed = useCoachFeed(store, status);
  const [gone, setGone] = useState<ReadonlySet<string>>(() => new Set());
  const cards = useMemo(() => (feed.data?.pages.flatMap((p) => p.items) ?? []).filter((c) => !gone.has(c.id)), [feed.data, gone]);

  if (feed.isError) {
    return (
      <Banner
        tone="warning"
        title={intl.formatMessage({ id: 'coach.error.load' })}
        action={
          <Button size="sm" loading={feed.isFetching} onClick={() => void feed.refetch()}>
            {intl.formatMessage({ id: 'coach.retry' })}
          </Button>
        }
      />
    );
  }
  if (feed.isPending) {
    return (
      <div role="status" aria-label={intl.formatMessage({ id: 'coach.loading' })}>
        <Skeleton shape="block" height="160px" />
      </div>
    );
  }
  if (cards.length === 0) {
    return <EmptyState icon={Lightbulb} title={intl.formatMessage({ id: `coach.empty.${status}` })} />;
  }
  return (
    <>
      <ul className={`tz-insights ${styles.list}`} aria-label={intl.formatMessage({ id: `coach.tab.${status}` })}>
        {cards.map((c) => (
          <CoachCardRow key={c.id} store={store} card={c} onGone={(id) => setGone((g) => new Set(g).add(id))} />
        ))}
      </ul>
      {feed.hasNextPage && (
        <div className={styles.more}>
          <Button loading={feed.isFetchingNextPage} onClick={() => void feed.fetchNextPage()}>
            {intl.formatMessage({ id: 'coach.more' })}
          </Button>
        </div>
      )}
    </>
  );
}
