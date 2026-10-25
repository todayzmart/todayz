import type { AdminCancelReason } from '@todayz/api-client/gen/model/adminCancelReason';
import type { AdminCancelOrderRequest } from '@todayz/api-client/gen/model/adminCancelOrderRequest';
import type { AdminStrikeSide } from '@todayz/api-client/gen/model/adminStrikeSide';
import { Button, Dialog, Segmented } from '@todayz/ui';
import { useId, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { CANCEL_REASONS, STRIKE_SIDES, useOrderLabels } from './labels';
import css from '../app/page.module.css';

/** The contract bound on AdminCancelOrderRequest.note. */
export const MAX_NOTE = 200;

export interface CancelDialogProps {
  open: boolean;
  number: string;
  pending: boolean;
  error: string | null;
  onConfirm: (body: AdminCancelOrderRequest) => void;
  onClose: () => void;
}

/**
 * Admin cancel (03 §4.2, D-20): a reason, who takes the strike (none by default, so a strike is
 * always a choice), and a note that `other` requires. The server refunds credits and audits it.
 */
export function CancelDialog({ open, number, pending, error, onConfirm, onClose }: CancelDialogProps) {
  const intl = useIntl();
  const labels = useOrderLabels();
  const id = useId();
  const [reason, setReason] = useState<AdminCancelReason | ''>('');
  const [strike, setStrike] = useState<AdminStrikeSide>('none');
  const [note, setNote] = useState('');
  const [touched, setTouched] = useState(false);
  const clean = note.trim();
  const reasonMissing = reason === '';
  const noteMissing = reason === 'other' && clean.length === 0;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (reasonMissing || noteMissing || pending) return;
    onConfirm({ reason, strike, ...(clean ? { note: clean } : {}) });
  };
  const close = () => {
    setReason('');
    setStrike('none');
    setNote('');
    setTouched(false);
    onClose();
  };
  return (
    <Dialog
      open={open}
      onClose={close}
      title={intl.formatMessage({ id: 'orders.cancel.title' }, { number })}
      description={intl.formatMessage({ id: 'orders.cancel.body' })}
      closeLabel={intl.formatMessage({ id: 'common.close' })}
      footer={
        <>
          <Button variant="ghost" onClick={close}>
            {intl.formatMessage({ id: 'orders.cancel.keep' })}
          </Button>
          <Button type="submit" form={id} variant="danger" loading={pending} loadingLabel={intl.formatMessage({ id: 'common.saving' })}>
            {intl.formatMessage({ id: 'orders.cancel.confirm' })}
          </Button>
        </>
      }
    >
      <form id={id} onSubmit={submit} noValidate className={css.stack}>
        <div className="tz-field">
          <label className="tz-field-label" htmlFor={`${id}-reason`}>
            {intl.formatMessage({ id: 'orders.cancel.reason' })}
          </label>
          <select
            id={`${id}-reason`}
            className="tz-input"
            value={reason}
            aria-invalid={touched && reasonMissing}
            onChange={(e) => setReason(e.target.value as AdminCancelReason | '')}
          >
            <option value="">{intl.formatMessage({ id: 'orders.cancel.pickReason' })}</option>
            {CANCEL_REASONS.map((r) => (
              <option key={r} value={r}>
                {labels.reason(r)}
              </option>
            ))}
          </select>
          {touched && reasonMissing && <p className="tz-field-error">{intl.formatMessage({ id: 'orders.cancel.reasonRequired' })}</p>}
        </div>
        <div className="tz-field">
          <Segmented
            label={intl.formatMessage({ id: 'orders.cancel.strike' })}
            options={STRIKE_SIDES.map((s) => ({ value: s, label: labels.side(s) }))}
            value={strike}
            onChange={setStrike}
          />
          <p className="tz-field-help">{intl.formatMessage({ id: 'orders.cancel.strikeHelp' })}</p>
        </div>
        <div className="tz-field">
          <label className="tz-field-label" htmlFor={`${id}-note`}>
            {intl.formatMessage({ id: 'orders.cancel.note' })}
          </label>
          <textarea
            id={`${id}-note`}
            className="tz-input"
            rows={2}
            maxLength={MAX_NOTE}
            value={note}
            aria-invalid={touched && noteMissing}
            aria-describedby={`${id}-note-help`}
            onChange={(e) => setNote(e.target.value)}
          />
          <p id={`${id}-note-help`} className={touched && noteMissing ? 'tz-field-error' : 'tz-field-help'}>
            {touched && noteMissing ? intl.formatMessage({ id: 'orders.cancel.noteRequired' }) : intl.formatMessage({ id: 'orders.cancel.noteHelp' })}
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
