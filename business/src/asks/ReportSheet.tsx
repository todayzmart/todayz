// B-20 "Report this ask": confirm sheet with a reason; afterwards this store gets no more asks from
// that customer. The customer is never told. Nothing is preselected.
import type { AskReportCreateReason } from '@todayz/api-client/gen/model/askReportCreateReason';
import type { StoreAsk } from '@todayz/api-client/gen/model/storeAsk';
import { Button, Sheet } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { askErrorKey, asksKey, sendReport } from './api';
import styles from './asks.module.css';

const REASONS: readonly AskReportCreateReason[] = ['spam', 'abusive', 'not_a_real_ask', 'other'];

export function ReportSheet({ store, ask, onClose }: { store: CurrentStore; ask: StoreAsk; onClose: () => void }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const [reason, setReason] = useState<AskReportCreateReason | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);

  const onConfirm = () => {
    if (reason === null) return;
    setBusy(true);
    setError(null);
    sendReport(store, ask.askId, reason)
      .then(() => {
        void qc.invalidateQueries({ queryKey: asksKey(store.sid) });
        onClose();
      })
      .catch((err: unknown) => setError(err))
      .finally(() => setBusy(false));
  };

  return (
    <Sheet
      open
      onClose={onClose}
      title={intl.formatMessage({ id: 'asks.report.title' })}
      description={intl.formatMessage({ id: 'asks.report.body' })}
      closeLabel={intl.formatMessage({ id: 'asks.sheet.close' })}
      footer={
        <>
          <Button onClick={onClose}>{intl.formatMessage({ id: 'asks.sheet.back' })}</Button>
          <Button variant="danger" disabled={reason === null} loading={busy} onClick={onConfirm}>
            {intl.formatMessage({ id: 'asks.report.confirm' })}
          </Button>
        </>
      }
    >
      <fieldset className={styles.reasons}>
        <legend className={styles.label}>{intl.formatMessage({ id: 'asks.report.reason' })}</legend>
        {REASONS.map((r) => (
          <label key={r} className={styles.reason}>
            <input type="radio" name="ask-report" value={r} checked={reason === r} onChange={() => setReason(r)} />
            <span>{intl.formatMessage({ id: `asks.report.reason.${r}` })}</span>
          </label>
        ))}
      </fieldset>
      {error !== null && (
        <p role="alert" className="tz-field-error">
          {intl.formatMessage({ id: `asks.error.${askErrorKey(error)}` })}
        </p>
      )}
    </Sheet>
  );
}
