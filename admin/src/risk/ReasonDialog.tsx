import { Banner, Button, Dialog } from '@todayz/ui';
import { useId, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import css from '../app/page.module.css';

export interface ReasonDialogProps {
  open: boolean;
  title: string;
  body: string;
  confirmLabel: string;
  /** The contract's bound on this reason. */
  max: number;
  danger: boolean;
  /** Shown above the reason, e.g. what clearing a refused reward lets happen. */
  warning?: string | undefined;
  pending: boolean;
  error: string | null;
  onConfirm: (reason: string) => void;
  onClose: () => void;
}

/** A move that needs a reason (D-225): required, trimmed, and kept on the row and in the audit log. */
export function ReasonDialog({ open, title, body, confirmLabel, max, danger, warning, pending, error, onConfirm, onClose }: ReasonDialogProps) {
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
            {intl.formatMessage({ id: 'risk.dialog.keep' })}
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
        {warning && <Banner tone="warning">{warning}</Banner>}
        <div className="tz-field">
          <label className="tz-field-label" htmlFor={`${id}-reason`}>
            {intl.formatMessage({ id: 'risk.dialog.reason' })}
          </label>
          <textarea
            id={`${id}-reason`}
            className="tz-input"
            rows={3}
            maxLength={max}
            value={reason}
            aria-invalid={touched && missing}
            aria-describedby={`${id}-reason-help`}
            onChange={(e) => setReason(e.target.value)}
          />
          <p id={`${id}-reason-help`} className={touched && missing ? 'tz-field-error' : 'tz-field-help'}>
            {touched && missing
              ? intl.formatMessage({ id: 'risk.dialog.reasonRequired' })
              : intl.formatMessage({ id: 'risk.dialog.reasonHelp' }, { max })}
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
