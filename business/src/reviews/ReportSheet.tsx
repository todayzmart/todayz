import { Button, Sheet, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { useId, useState } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { REPORT_NOTE_MAX, REPORT_REASONS, REPORT_TEXT_MAX, type ReportReason } from './limits';
import { sendReport } from './queries';
import { ReviewsError } from './ReviewsError';
import styles from './reviews.module.css';

/** Report a review to TodayZ: a reason and an optional note. */
export function ReportSheet({ store, reviewId, onClose }: { store: CurrentStore; reviewId: string; onClose: () => void }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const noteId = useId();
  const [reason, setReason] = useState<ReportReason | null>(null);
  const [note, setNote] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);

  const submit = async () => {
    if (!reason) return;
    setPending(true);
    setError(null);
    try {
      const n = note.trim();
      await sendReport(qc, store, reviewId, (n ? `${reason}: ${n}` : reason).slice(0, REPORT_TEXT_MAX));
      toast.show({ message: intl.formatMessage({ id: 'reviews.toast.reported' }) });
      onClose();
    } catch (err) {
      setError(err);
    } finally {
      setPending(false);
    }
  };

  return (
    <Sheet
      open
      onClose={onClose}
      title={intl.formatMessage({ id: 'reviews.report.title' })}
      description={intl.formatMessage({ id: 'reviews.report.body' })}
      closeLabel={intl.formatMessage({ id: 'reviews.close' })}
    >
      <form
        className={styles.sheetBody}
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <fieldset className={styles.reasons}>
          <legend className="tz-field-label">{intl.formatMessage({ id: 'reviews.report.reason' })}</legend>
          {REPORT_REASONS.map((r) => (
            <label key={r} className={styles.reasonRow}>
              <input type="radio" name="reason" value={r} checked={reason === r} onChange={() => setReason(r)} />
              <span>{intl.formatMessage({ id: `reviews.report.reasons.${r}` })}</span>
            </label>
          ))}
        </fieldset>
        <div className="tz-field">
          <label className="tz-field-label" htmlFor={noteId}>
            {intl.formatMessage({ id: 'reviews.report.note' })}
          </label>
          <textarea id={noteId} className="tz-input" rows={3} maxLength={REPORT_NOTE_MAX} value={note} onChange={(e) => setNote(e.target.value)} />
        </div>
        <ReviewsError error={error} />
        <div className={styles.row}>
          <Button type="submit" variant="primary" loading={pending} disabled={!reason}>
            {intl.formatMessage({ id: 'reviews.report.submit' })}
          </Button>
          <Button variant="ghost" onClick={onClose}>
            {intl.formatMessage({ id: 'reviews.cancel' })}
          </Button>
        </div>
      </form>
    </Sheet>
  );
}
