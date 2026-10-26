// B-20 Asks inbox (D-101), the Asks tab of B-04. Open cards (item, quantity, need by, distance band,
// time left from serverNow), then "Done" for the day (replied and missed) and the earlier ones. Never a
// customer's name, phone, address or exact distance: the API sends none.
import type { StoreAsk } from '@todayz/api-client/gen/model/storeAsk';
import { formatMoney } from '@todayz/i18n';
import { Button, EmptyState, Skeleton, StatusPill, VisuallyHidden } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { BellOff, Clock, MessageCircleQuestion } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { mmss } from '../overview/format';
import { useSecondsLeft } from '../orders/queries';
import type { CurrentStore } from '../orders/store';
import { Icon } from '../shell/Icon';
import { useStoreSettings } from '../stores/queries';
import { useDoneAsks, useOpenAsks } from './api';
import { canSeeAskSettings } from './perm';
import { ReplySheet } from './ReplySheet';
import { ReportSheet } from './ReportSheet';
import { doneToday } from './time';
import styles from './asks.module.css';

type Sheet = { kind: 'reply' | 'report'; ask: StoreAsk } | null;

export function AsksInbox({ store }: { store: CurrentStore }) {
  const intl = useIntl();
  const open = useOpenAsks(store, true);
  const done = useDoneAsks(store);
  const manages = canSeeAskSettings(store.role);
  const settings = useStoreSettings(store.bid, store.sid, manages);
  const [sheet, setSheet] = useState<Sheet>(null);
  const [earlier, setEarlier] = useState(false);

  if (open.isError) {
    return (
      <EmptyState
        icon={MessageCircleQuestion}
        title={intl.formatMessage({ id: 'asks.error.load' })}
        action={<Button loading={open.isFetching} onClick={() => void open.refetch()}>{intl.formatMessage({ id: 'asks.retry' })}</Button>}
      />
    );
  }
  if (!open.data) {
    return (
      <div role="status" aria-label={intl.formatMessage({ id: 'asks.loading' })}>
        <Skeleton height="6rem" />
      </div>
    );
  }

  const offsetMs = Date.parse(open.data.serverNow) - open.dataUpdatedAt;
  const serverNow = open.data.serverNow;
  const finished = (done.data?.pages ?? []).flatMap((p) => p.asks).filter((a) => a.status !== 'open');
  const today = finished.filter((a) => doneToday(a, serverNow));
  const older = finished.filter((a) => !doneToday(a, serverNow));
  const muted = settings.data?.asksMuted === true;

  return (
    <div className={styles.inbox}>
      {muted && (
        <div className={`tz-card ${styles.muted}`} role="status">
          <Icon icon={BellOff} className="tz-ico-sm" />
          <span>{intl.formatMessage({ id: 'asks.muted' })}</span>
          <Link to="/settings" className="tz-btn tz-btn-ghost tz-btn-sm">
            {intl.formatMessage({ id: 'asks.muted.settings' })}
          </Link>
        </div>
      )}

      {open.data.asks.length === 0 ? (
        !muted && (
          <EmptyState
            icon={MessageCircleQuestion}
            title={intl.formatMessage({ id: 'asks.empty.title' })}
            description={intl.formatMessage({ id: 'asks.empty.body' })}
          />
        )
      ) : (
        <ul className={styles.list} aria-label={intl.formatMessage({ id: 'asks.open' })}>
          {open.data.asks.map((a) => (
            <li key={a.askId}>
              <OpenCard ask={a} offsetMs={offsetMs} onReply={() => setSheet({ kind: 'reply', ask: a })} onReport={() => setSheet({ kind: 'report', ask: a })} />
            </li>
          ))}
        </ul>
      )}

      {today.length > 0 && (
        <section className={styles.section} aria-labelledby="asks-done">
          <h2 id="asks-done" className={styles.sectionTitle}>
            {intl.formatMessage({ id: 'asks.done' })}
          </h2>
          <DoneList asks={today} onReport={(a) => setSheet({ kind: 'report', ask: a })} />
        </section>
      )}

      {(older.length > 0 || done.hasNextPage) && (
        <section className={styles.section} aria-labelledby="asks-earlier">
          <h2 id="asks-earlier" className={styles.sectionTitle}>
            {intl.formatMessage({ id: 'asks.earlier' })}
          </h2>
          {earlier ? (
            <>
              <DoneList asks={older} onReport={(a) => setSheet({ kind: 'report', ask: a })} />
              {done.hasNextPage && (
                <Button size="sm" loading={done.isFetchingNextPage} onClick={() => void done.fetchNextPage()}>
                  {intl.formatMessage({ id: 'asks.more' })}
                </Button>
              )}
            </>
          ) : (
            <Button
              size="sm"
              variant="ghost"
              className={styles.showEarlier}
              onClick={() => {
                setEarlier(true);
                if (older.length === 0 && done.hasNextPage) void done.fetchNextPage();
              }}
            >
              {intl.formatMessage({ id: 'asks.earlier.show' })}
            </Button>
          )}
        </section>
      )}

      {sheet?.kind === 'reply' && <ReplySheet store={store} ask={sheet.ask} offsetMs={offsetMs} onClose={() => setSheet(null)} />}
      {sheet?.kind === 'report' && <ReportSheet store={store} ask={sheet.ask} onClose={() => setSheet(null)} />}
    </div>
  );
}

function AskFacts({ ask }: { ask: StoreAsk }) {
  const intl = useIntl();
  return (
    <p className={styles.facts}>
      {intl.formatMessage({ id: 'asks.quantity' }, { n: ask.quantity })}
      {ask.needBy && ` · ${intl.formatMessage({ id: `asks.need.${ask.needBy}` })}`}
      {` · ${intl.formatMessage({ id: `asks.band.${ask.distanceBand}` })}`}
    </p>
  );
}

function OpenCard({ ask, offsetMs, onReply, onReport }: { ask: StoreAsk; offsetMs: number; onReply: () => void; onReport: () => void }) {
  const intl = useIntl();
  const left = useSecondsLeft(ask.replyBy, offsetMs);
  const over = left === 0;
  const time = mmss(left ?? 0);
  return (
    <article className="tz-ocard" aria-label={ask.itemName}>
      <div className="tz-ocard-top">
        <span className={styles.item}>{ask.itemName}</span>
        {left !== null && (
          <span className="tz-timer" data-testid="ask-countdown">
            <Icon icon={Clock} className="tz-ico-sm" />
            <span aria-hidden="true">{time}</span>
            <VisuallyHidden>
              {over ? intl.formatMessage({ id: 'asks.card.over' }) : intl.formatMessage({ id: 'asks.card.timeLeft' }, { time })}
            </VisuallyHidden>
          </span>
        )}
      </div>
      <AskFacts ask={ask} />
      {ask.available !== null && ask.available < ask.quantity && (
        <p className={styles.short}>{intl.formatMessage({ id: 'asks.reply.youHave' }, { n: Math.max(0, ask.available) })}</p>
      )}
      <div className={`tz-ocard-actions ${styles.actions}`}>
        <Button size="sm" variant="ghost" onClick={onReport} aria-label={intl.formatMessage({ id: 'asks.card.reportA11y' }, { item: ask.itemName })}>
          {intl.formatMessage({ id: 'asks.card.report' })}
        </Button>
        <Button size="sm" variant="primary" disabled={over} onClick={onReply} aria-label={intl.formatMessage({ id: 'asks.card.replyA11y' }, { item: ask.itemName })}>
          {intl.formatMessage({ id: 'asks.card.reply' })}
        </Button>
      </div>
    </article>
  );
}

function DoneList({ asks, onReport }: { asks: readonly StoreAsk[]; onReport: (a: StoreAsk) => void }) {
  const intl = useIntl();
  return (
    <ul className={styles.list}>
      {asks.map((a) => (
        <li key={a.askId} className={`tz-card ${styles.doneRow}`}>
          <div className={styles.doneTop}>
            <span className={styles.item}>{a.itemName}</span>
            <StatusPill tone={a.status === 'missed' ? 'neutral' : a.answer === 'yes' ? 'accepted' : 'rejected'}>
              {intl.formatMessage({ id: a.status === 'missed' ? 'asks.status.missed' : a.answer === 'yes' ? 'asks.status.yes' : 'asks.status.no' })}
            </StatusPill>
          </div>
          <AskFacts ask={a} />
          {a.answer === 'yes' && a.pricePaise !== null && <p className="tz-price">{formatMoney(a.pricePaise)}</p>}
          {a.reported ? (
            <span className="tz-meta">{intl.formatMessage({ id: 'asks.reported' })}</span>
          ) : (
            <Button size="sm" variant="ghost" className={styles.showEarlier} onClick={() => onReport(a)} aria-label={intl.formatMessage({ id: 'asks.card.reportA11y' }, { item: a.itemName })}>
              {intl.formatMessage({ id: 'asks.card.report' })}
            </Button>
          )}
        </li>
      ))}
    </ul>
  );
}
