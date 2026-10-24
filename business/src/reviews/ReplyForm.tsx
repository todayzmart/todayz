import { useToast, Button } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { useId, useState } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import type { StoreReview } from '@todayz/api-client/gen/model/storeReview';
import { cleanReply, REPLY_MAX } from './limits';
import { saveReply } from './queries';
import { ReviewsError } from './ReviewsError';
import styles from './reviews.module.css';

/** Writes or edits the shop's one reply: POST the first time, PATCH after. */
export function ReplyForm({ store, review, onDone }: { store: CurrentStore; review: StoreReview; onDone: () => void }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const id = useId();
  const [text, setText] = useState(review.reply?.text ?? '');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [empty, setEmpty] = useState(false);
  const editing = review.reply !== null;

  const submit = async () => {
    const clean = cleanReply(text);
    if (clean === null) {
      setEmpty(true);
      return;
    }
    setEmpty(false);
    setPending(true);
    setError(null);
    try {
      await saveReply(qc, store, review, clean);
      toast.show({ message: intl.formatMessage({ id: editing ? 'reviews.toast.replyUpdated' : 'reviews.toast.replySent' }) });
      onDone();
    } catch (err) {
      setError(err);
    } finally {
      setPending(false);
    }
  };

  return (
    <form
      className={styles.reply}
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <div className="tz-field">
        <label className="tz-field-label" htmlFor={id}>
          {intl.formatMessage({ id: editing ? 'reviews.reply.editLabel' : 'reviews.reply.label' })}
        </label>
        <textarea
          id={id}
          className={empty ? 'tz-input tz-input-invalid' : 'tz-input'}
          rows={3}
          maxLength={REPLY_MAX}
          value={text}
          aria-invalid={empty || undefined}
          onChange={(e) => {
            setText(e.target.value);
            setEmpty(false);
          }}
        />
        <span className="tz-field-help">{intl.formatMessage({ id: 'reviews.reply.counter' }, { n: text.length, max: REPLY_MAX })}</span>
        {empty && (
          <span role="alert" className="tz-field-error">
            {intl.formatMessage({ id: 'reviews.reply.empty' })}
          </span>
        )}
      </div>
      <ReviewsError error={error} />
      <div className={styles.row}>
        <Button type="submit" variant="primary" size="sm" loading={pending}>
          {intl.formatMessage({ id: editing ? 'reviews.reply.save' : 'reviews.reply.send' })}
        </Button>
        <Button size="sm" variant="ghost" onClick={onDone}>
          {intl.formatMessage({ id: 'reviews.cancel' })}
        </Button>
      </div>
    </form>
  );
}
