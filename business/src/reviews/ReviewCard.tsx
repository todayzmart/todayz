import { StatusPill, Button } from '@todayz/ui';
import type { StoreReview } from '@todayz/api-client/gen/model/storeReview';
import { Flag, MessageSquareReply, Pencil } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { whenText } from '../stock/labels';
import { ReplyForm } from './ReplyForm';
import { Stars } from './Stars';
import styles from './reviews.module.css';

/** One review: stars, tags, text, the shop's reply, and the reply and report controls. */
export function ReviewCard({ store, review, onReport }: { store: CurrentStore; review: StoreReview; onReport: (id: string) => void }) {
  const intl = useIntl();
  const [replying, setReplying] = useState(false);
  const who = review.customerName?.trim() || intl.formatMessage({ id: 'reviews.customer' });
  const tag = (t: string) => (intl.messages[`reviews.tag.${t}`] ? intl.formatMessage({ id: `reviews.tag.${t}` }) : null);

  return (
    <li className={styles.card} aria-label={intl.formatMessage({ id: 'reviews.card.label' }, { who, n: review.stars })}>
      <div className={styles.cardHead}>
        <Stars value={review.stars} />
        <span className={styles.sub}>
          {who} · {whenText(intl, review.createdAt)}
        </span>
        {review.reported && <StatusPill>{intl.formatMessage({ id: 'reviews.reported.pill' })}</StatusPill>}
      </div>

      {review.tags.length > 0 && (
        <ul className={styles.tags} aria-label={intl.formatMessage({ id: 'reviews.tags.label' })}>
          {review.tags.flatMap((t) => {
            const label = tag(t);
            return label ? [<li key={t} className={styles.tag}>{label}</li>] : [];
          })}
        </ul>
      )}
      {review.text && <p className={styles.text}>{review.text}</p>}

      {review.reply && !replying && (
        <div className={styles.replyBox}>
          <p className={styles.replyHead}>
            {intl.formatMessage({ id: 'reviews.reply.yours' })} · {whenText(intl, review.reply.editedAt ?? review.reply.createdAt)}
          </p>
          <p className={styles.text}>{review.reply.text}</p>
        </div>
      )}

      {replying && <ReplyForm store={store} review={review} onDone={() => setReplying(false)} />}

      {!replying && (
        <div className={styles.row}>
          <Button size="sm" icon={review.reply ? Pencil : MessageSquareReply} onClick={() => setReplying(true)}>
            {intl.formatMessage({ id: review.reply ? 'reviews.reply.edit' : 'reviews.reply.start' })}
          </Button>
          <Button size="sm" variant="ghost" icon={Flag} disabled={review.reported} onClick={() => onReport(review.id)}>
            {intl.formatMessage({ id: review.reported ? 'reviews.reported.button' : 'reviews.report.button' })}
          </Button>
        </div>
      )}
    </li>
  );
}
