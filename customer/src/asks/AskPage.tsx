// C-26 `/asks/$askId`: progress ("Asked 4 shops · 2 replied · best ₹58"), the time left for shops to
// reply, and the Yes cards. Live over /v1/me/stream with the package's polling fallback. A No shows only
// in the replied count. Every countdown runs from the response's serverNow (06 G9), never the device clock.
import type { Ask } from '@todayz/api-client/gen/model/ask';
import type { AskDetail } from '@todayz/api-client/gen/model/askDetail';
import type { AskReply } from '@todayz/api-client/gen/model/askReply';
import { ApiError } from '@todayz/api-client';
import { SessionPending, useSessionState } from '@todayz/auth';
import { formatClock, formatDistance, formatMoney, formatRelative } from '@todayz/i18n';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, EmptyState, Skeleton, StatusPill } from '@todayz/ui';
import { ArrowLeft, Hourglass, PackageSearch, SearchX } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { useLocale } from '../app/locale';
import { RouterLink } from '../home/RouterLink';
import { formatMmSs, useServerCountdown } from '../orders/clock';
import { askKey, closeAsk, loadAsk, storedAsk, useMyAskStream } from './api';
import { useBookReply } from './book';
import { progressLine } from './progress';
import styles from './asks.module.css';

export function AskPage({ askId }: { askId: string }) {
  const { status } = useSessionState();
  useMyAskStream();

  if (status === 'signedOut') return <SignedOut next={`/asks/${askId}`} />;
  if (status === 'unknown') return <Pending />;
  return <AskBody askId={askId} />;
}

function AskBody({ askId }: { askId: string }) {
  const intl = useIntl();
  const q = useQuery<AskDetail, Error>({
    queryKey: askKey(askId),
    queryFn: () => loadAsk(askId),
    // The stream is the fast path; this is the slow one while the ask is open (02 §5).
    refetchInterval: (query) => (query.state.data?.ask.status === 'open' ? 30_000 : false),
  });

  return (
    <div className={styles.page}>
      <RouterLink href="/asks" className={styles.back}>
        <ArrowLeft size={16} strokeWidth={1.5} aria-hidden />
        {intl.formatMessage({ id: 'asks.back' })}
      </RouterLink>
      {q.isPending && <Skeleton shape="block" height="160px" />}
      {q.isError &&
        (q.error instanceof ApiError && q.error.status === 404 ? (
          <EmptyState icon={SearchX} title={intl.formatMessage({ id: 'asks.gone' })} />
        ) : (
          <EmptyState
            icon={SearchX}
            title={intl.formatMessage({ id: 'asks.error.load' })}
           
            action={
              <Button variant="soft" onClick={() => void q.refetch()}>
                {intl.formatMessage({ id: 'asks.retry' })}
              </Button>
            }
          />
        ))}
      {q.data && <AskView detail={q.data} />}
    </div>
  );
}

function AskView({ detail }: { detail: AskDetail }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const { ask, replies, serverNow } = detail;
  const left = useServerCountdown(ask.status === 'open' ? ask.windowEndsAt : null, serverNow);
  const { book, sheet } = useBookReply();
  const [closing, setClosing] = useState(false);
  const [closeFailed, setCloseFailed] = useState(false);
  const soonest = soonestReply(replies);

  const onClose = () => {
    setClosing(true);
    setCloseFailed(false);
    closeAsk(ask.askId)
      .then((d) => storedAsk(qc, d))
      .catch(() => setCloseFailed(true))
      .finally(() => setClosing(false));
  };

  return (
    <>
      <header className={styles.head}>
        <h1 className="tz-page-title">{ask.itemName}</h1>
        <p className="tz-meta">
          {intl.formatMessage({ id: 'asks.quantity' }, { n: ask.quantity })}
          {ask.needBy && ` · ${intl.formatMessage({ id: `asks.need.${ask.needBy}` })}`}
        </p>
        <StatusPill tone={ask.status === 'open' ? 'pending' : 'neutral'}>
          {intl.formatMessage({ id: ask.status === 'open' ? 'asks.status.open' : 'asks.status.closed' })}
        </StatusPill>
      </header>

      <section className="tz-card" aria-live="polite">
        <p className={styles.progress}>{progressLine(intl, ask)}</p>
        {left !== null && left > 0 && (
          <p className="tz-meta">{intl.formatMessage({ id: 'asks.windowLeft' }, { time: formatMmSs(left) })}</p>
        )}
      </section>

      {replies.length > 0 ? (
        <ul className={styles.replies} aria-label={intl.formatMessage({ id: 'asks.replies' })}>
          {replies.map((r) => (
            <li key={r.askReplyId}>
              <ReplyCard ask={ask} reply={r} serverNow={serverNow} soonest={soonest === r.askReplyId} onBook={book} />
            </li>
          ))}
        </ul>
      ) : ask.status === 'open' ? (
        <EmptyState icon={Hourglass} title={intl.formatMessage({ id: 'asks.waiting' }, { min: waitMinutes(ask, serverNow) })} />
      ) : (
        <EmptyState icon={PackageSearch} title={intl.formatMessage({ id: 'asks.none' })} />
      )}

      {ask.status === 'open' && (
        <Button variant="ghost" block loading={closing} onClick={onClose}>
          {intl.formatMessage({ id: 'asks.close' })}
        </Button>
      )}
      {closeFailed && (
        <p className={styles.problem} role="alert">
          {intl.formatMessage({ id: 'asks.error.close' })}
        </p>
      )}
      {sheet}
    </>
  );
}

/**
 * The minutes in "Most reply within {min} min": the reply window as the server set it on this ask
 * (ask_reply_window_min), so the number is never typed into the app.
 */
function waitMinutes(ask: Ask, serverNow: string): number {
  const end = ask.windowEndsAt ? Date.parse(ask.windowEndsAt) : Number.NaN;
  const start = Date.parse(ask.createdAt);
  const span = Number.isFinite(end) ? end - start : Date.parse(serverNow) - start;
  return Math.max(1, Math.round(span / 60_000));
}

/** "Ready soonest" goes on the earliest ready-by, only when there is more than one to compare. */
function soonestReply(replies: readonly AskReply[]): string | null {
  const timed = replies.filter((r) => r.readyBy !== null);
  if (timed.length < 2) return null;
  return timed.reduce((a, b) => (Date.parse(b.readyBy ?? '') < Date.parse(a.readyBy ?? '') ? b : a)).askReplyId;
}

function ReplyCard({
  ask,
  reply,
  serverNow,
  soonest,
  onBook,
}: {
  ask: Ask;
  reply: AskReply;
  serverNow: string;
  soonest: boolean;
  onBook: (ask: Ask, reply: AskReply, priceValid: boolean) => void;
}) {
  const intl = useIntl();
  const { locale } = useLocale();
  const held = useServerCountdown(reply.pricePaise !== null ? reply.priceValidUntil : null, serverNow);
  const priceValid = reply.pricePaise !== null && held !== null && held > 0;
  const expired = reply.pricePaise !== null && !priceValid;

  return (
    <article className={`tz-card ${styles.reply}`}>
      <div className={styles.replyHead}>
        <h2 className={styles.shop}>{reply.storeName}</h2>
        {soonest && <span className="tz-tag tz-tag-open">{intl.formatMessage({ id: 'asks.reply.soonest' })}</span>}
      </div>
      <p className="tz-meta">
        {formatDistance(reply.distanceM)} · {intl.formatMessage({ id: 'asks.reply.replied' }, { ago: formatRelative(reply.repliedAt, serverNow, locale) })}
      </p>
      {reply.pricePaise === null ? (
        <p className={styles.price}>{intl.formatMessage({ id: 'asks.reply.noPrice' })}</p>
      ) : expired ? (
        <p className={styles.price}>
          {intl.formatMessage({ id: 'asks.reply.priceChanged' })}
          {reply.listedPricePaise !== null && ` · ${formatMoney(reply.listedPricePaise)}`}
        </p>
      ) : (
        <p className={styles.price}>
          {intl.formatMessage({ id: 'asks.reply.price' }, { price: formatMoney(reply.pricePaise) })}
          {held !== null && <span className="tz-meta"> · {intl.formatMessage({ id: 'asks.reply.held' }, { time: formatMmSs(held) })}</span>}
        </p>
      )}
      {reply.readyBy && <p className="tz-meta">{intl.formatMessage({ id: 'asks.reply.readyBy' }, { time: formatClock(reply.readyBy) })}</p>}
      {reply.orderId ? (
        <RouterLink href={`/orders/${reply.orderId}`} className="tz-btn tz-btn-soft tz-btn-block">
          {intl.formatMessage({ id: 'asks.reply.viewOrder' })}
        </RouterLink>
      ) : reply.inStoreOnly ? (
        <p className={styles.note}>{intl.formatMessage({ id: 'asks.reply.inStore' })}</p>
      ) : reply.bookable && reply.storeItemId ? (
        <Button variant="primary" block onClick={() => onBook(ask, reply, priceValid)}>
          {intl.formatMessage({ id: 'asks.reply.book' })}
        </Button>
      ) : null}
    </article>
  );
}

function SignedOut({ next }: { next: string }) {
  const intl = useIntl();
  return (
    <div className={styles.page}>
      <EmptyState
        icon={PackageSearch}
        title={intl.formatMessage({ id: 'asks.signedOut.title' })}
        description={intl.formatMessage({ id: 'asks.signedOut.body' })}
       
        action={
          <RouterLink href={`/sign-in?next=${encodeURIComponent(next)}`} className="tz-btn tz-btn-primary">
            {intl.formatMessage({ id: 'asks.signedOut.cta' })}
          </RouterLink>
        }
      />
    </div>
  );
}

function Pending() {
  const intl = useIntl();
  return (
    <SessionPending
      className={styles.page}
      loadingLabel={intl.formatMessage({ id: 'asks.loading' })}
      offlineTitle={intl.formatMessage({ id: 'app.offline.title' })}
      offlineMessage={intl.formatMessage({ id: 'app.offline.body' })}
      retryLabel={intl.formatMessage({ id: 'app.pending.retry' })}
    />
  );
}

export { SignedOut as AsksSignedOut, Pending as AsksPending };
