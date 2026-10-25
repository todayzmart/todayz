import { Button, Dialog } from '@todayz/ui';
import { useId, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import css from '../app/page.module.css';

/** The contract bound on ModerationNote. */
export const MAX_REASON = 300;

export interface ReasonDialogProps {
  open: boolean;
  title: string;
  body: string;
  confirmLabel: string;
  /** Hiding is the destructive move; restoring is not. */
  danger: boolean;
  pending: boolean;
  error: string | null;
  onConfirm: (reason: string) => void;
  onClose: () => void;
}

/** Hide or restore with a reason (D-41): the reason is required and kept on the row and in the audit log. */
export function ReasonDialog({ open, title, body, confirmLabel, danger, pending, error, onConfirm, onClose }: ReasonDialogProps) {
  const intl = useIntl();
  const id = useId();
  const [reason, setReason] = useState('');
  const [touched, setTouched] = useState(false);
  const clean = reason.trim();
  const missing = clean.length === 0;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (missing || pending) return;
    onConfirm(clean);
  };
  const close = () => {
    setReason('');
    setTouched(false);
    onClose();
  };
  return (
    <Dialog
      open={open}
      onClose={close}
      title={title}
      description={body}
      closeLabel={intl.formatMessage({ id: 'common.close' })}
      footer={
        <>
          <Button variant="ghost" onClick={close}>
            {intl.formatMessage({ id: 'reviews.dialog.keep' })}
          </Button>
          <Button
            type="submit"
            form={id}
            variant={danger ? 'danger' : 'primary'}
            loading={pending}
            loadingLabel={intl.formatMessage({ id: 'common.saving' })}
          >
            {confirmLabel}
          </Button>
        </>
      }
    >
      <form id={id} onSubmit={submit} noValidate className={css.stack}>
        <div className="tz-field">
          <label className="tz-field-label" htmlFor={`${id}-reason`}>
            {intl.formatMessage({ id: 'reviews.dialog.reason' })}
          </label>
          <textarea
            id={`${id}-reason`}
            className="tz-input"
            rows={3}
            maxLength={MAX_REASON}
            value={reason}
            aria-invalid={touched && missing}
            aria-describedby={`${id}-reason-help`}
            onChange={(e) => setReason(e.target.value)}
          />
          <p id={`${id}-reason-help`} className={touched && missing ? 'tz-field-error' : 'tz-field-help'}>
            {touched && missing
              ? intl.formatMessage({ id: 'reviews.dialog.reasonRequired' })
              : intl.formatMessage({ id: 'reviews.dialog.reasonHelp' }, { max: MAX_REASON })}
          </p>
        </div>
        {error && (
          <p role="alert" className="tz-field-error">
            {error}
          </p>
        )}
      </form>
    </Dialog>
  );
}
