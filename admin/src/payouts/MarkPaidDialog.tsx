import type { AdminPayout } from '@todayz/api-client/gen/model/adminPayout';
import { Button, Dialog, TextField } from '@todayz/ui';
import { useId, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { money } from '../app/format';
import { MAX_REASON } from '../app/ReasonDialog';

/** The bank's transfer reference (payouts.utrPattern on the server). */
export const UTR = /^[A-Z0-9]{6,30}$/;

export interface MarkPaidDialogProps {
  payout: AdminPayout;
  pending: boolean;
  error: string | null;
  onConfirm: (utr: string, reason: string) => void;
  onClose: () => void;
}

/** Mark paid: the UTR of the transfer already made, and a reason. The amount is the one on screen. */
export function MarkPaidDialog({ payout, pending, error, onConfirm, onClose }: MarkPaidDialogProps) {
  const intl = useIntl();
  const id = useId();
  const [utr, setUtr] = useState('');
  const [reason, setReason] = useState('');
  const [touched, setTouched] = useState(false);
  const cleanUtr = utr.trim().toUpperCase();
  const cleanReason = reason.trim();
  const badUtr = !UTR.test(cleanUtr);
  const badReason = cleanReason.length === 0 || cleanReason.length > MAX_REASON;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!badUtr && !badReason && !pending) onConfirm(cleanUtr, cleanReason);
  };
  return (
    <Dialog
      open
      onClose={onClose}
      title={intl.formatMessage({ id: 'payouts.paid.title' }, { business: payout.businessName })}
      description={intl.formatMessage({ id: 'payouts.paid.body' }, { amount: money(payout.amountPaise) })}
      closeLabel={intl.formatMessage({ id: 'common.close' })}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {intl.formatMessage({ id: 'common.cancel' })}
          </Button>
          <Button type="submit" form={id} variant="primary" loading={pending} loadingLabel={intl.formatMessage({ id: 'common.saving' })}>
            {intl.formatMessage({ id: 'payouts.paid.confirm' }, { amount: money(payout.amountPaise) })}
          </Button>
        </>
      }
    >
      <form id={id} onSubmit={submit} noValidate>
        <TextField
          label={intl.formatMessage({ id: 'payouts.paid.utr' })}
          value={utr}
          maxLength={30}
          autoComplete="off"
          spellCheck={false}
          onChange={(e) => setUtr(e.target.value)}
          help={intl.formatMessage({ id: 'payouts.paid.utrHelp' })}
          error={touched && badUtr ? intl.formatMessage({ id: 'payouts.paid.utrInvalid' }) : undefined}
        />
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
            aria-invalid={touched && badReason}
            aria-describedby={`${id}-help`}
            onChange={(e) => setReason(e.target.value)}
          />
          <p id={`${id}-help`} className={touched && badReason ? 'tz-field-error' : 'tz-field-help'}>
            {touched && badReason ? intl.formatMessage({ id: 'common.reason.required' }) : intl.formatMessage({ id: 'common.reason.help' })}
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
