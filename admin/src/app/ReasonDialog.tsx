import { Button, Dialog } from '@todayz/ui';
import { useId, useState, type FormEvent, type ReactNode } from 'react';
import { useIntl } from 'react-intl';

/** The contract bound on every admin reason (AdminCustomerChange.reason). */
export const MAX_REASON = 500;

export interface ReasonDialogProps {
  open: boolean;
  title: string;
  description: ReactNode;
  confirmLabel: string;
  danger?: boolean;
  pending: boolean;
  /** The last attempt's failure, already in words. */
  error: string | null;
  onConfirm: (reason: string) => void;
  onClose: () => void;
}

/** A change that needs a written reason (block, unblock, strike reset). The reason is audited. */
export function ReasonDialog({ open, title, description, confirmLabel, danger, pending, error, onConfirm, onClose }: ReasonDialogProps) {
  const intl = useIntl();
  const id = useId();
  const [reason, setReason] = useState('');
  const [touched, setTouched] = useState(false);
  const clean = reason.trim();
  const invalid = clean.length === 0 || clean.length > MAX_REASON;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!invalid && !pending) onConfirm(clean);
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
      description={description}
      closeLabel={intl.formatMessage({ id: 'common.close' })}
      footer={
        <>
          <Button variant="ghost" onClick={close}>
            {intl.formatMessage({ id: 'common.cancel' })}
          </Button>
          <Button type="submit" form={id} variant={danger ? 'danger' : 'primary'} loading={pending} loadingLabel={intl.formatMessage({ id: 'common.saving' })}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      <form id={id} onSubmit={submit} noValidate>
        <div className="tz-field">
          <label className="tz-field-label" htmlFor={`${id}-reason`}>
            {intl.formatMessage({ id: 'common.reason.label' })}
          </label>
          <textarea
            id={`${id}-reason`}
            className="tz-input"
            rows={3}
            maxLength={MAX_REASON}
            value={reason}
            aria-invalid={touched && invalid}
            aria-describedby={`${id}-help`}
            onChange={(e) => setReason(e.target.value)}
          />
          <p id={`${id}-help`} className={touched && invalid ? 'tz-field-error' : 'tz-field-help'}>
            {touched && invalid ? intl.formatMessage({ id: 'common.reason.required' }) : intl.formatMessage({ id: 'common.reason.help' })}
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
