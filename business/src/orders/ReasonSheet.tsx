import { Button, Sheet, TextField } from '@todayz/ui';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { ActionError } from './ActionError';
import styles from './orders.module.css';

export interface ReasonSheetProps<R extends string> {
  open: boolean;
  onClose: () => void;
  title: string;
  body: string;
  reasons: readonly R[];
  confirmLabel: string;
  busy: boolean;
  error: unknown;
  /** `other` needs a note (D-60, D-68); the sheet enforces it before it calls this. */
  onConfirm: (reason: R, note: string | undefined) => void;
}

/** Reason picker for Reject (D-60) and the shop's Cancel (D-68). Nothing is preselected. */
export function ReasonSheet<R extends string>({ open, onClose, title, body, reasons, confirmLabel, busy, error, onConfirm }: ReasonSheetProps<R>) {
  const intl = useIntl();
  const [reason, setReason] = useState<R | null>(null);
  const [note, setNote] = useState('');
  const needsNote = reason === 'other';
  const trimmed = note.trim();
  const ready = reason !== null && (!needsNote || trimmed.length > 0);
  return (
    <Sheet
      open={open}
      onClose={onClose}
      title={title}
      description={body}
      closeLabel={intl.formatMessage({ id: 'orders.sheet.close' })}
      footer={
        <>
          <Button onClick={onClose}>{intl.formatMessage({ id: 'orders.sheet.back' })}</Button>
          <Button
            variant="danger"
            disabled={!ready}
            loading={busy}
            onClick={() => {
              if (reason !== null) onConfirm(reason, needsNote ? trimmed : undefined);
            }}
          >
            {confirmLabel}
          </Button>
        </>
      }
    >
      <fieldset className={styles.reasons}>
        <legend className={styles.legend}>{intl.formatMessage({ id: 'orders.sheet.reason' })}</legend>
        {reasons.map((r) => (
          <label key={r} className={styles.reason}>
            <input type="radio" name="reason" value={r} checked={reason === r} onChange={() => setReason(r)} />
            <span>{intl.formatMessage({ id: `orders.reason.${r}` })}</span>
          </label>
        ))}
      </fieldset>
      {needsNote && (
        <TextField
          label={intl.formatMessage({ id: 'orders.sheet.note' })}
          help={intl.formatMessage({ id: 'orders.sheet.noteHelp' })}
          value={note}
          maxLength={200}
          onChange={(e) => setNote(e.target.value)}
        />
      )}
      <ActionError error={error} />
    </Sheet>
  );
}
