import type { AdminLedgerAccount } from '@todayz/api-client/gen/model/adminLedgerAccount';
import type { CreditAdjustmentCreate } from '@todayz/api-client/gen/model/creditAdjustmentCreate';
import { ApiError } from '@todayz/api-client';
import { Button, Dialog, TextField, useToast } from '@todayz/ui';
import { useId, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { money } from '../app/format';
import { MAX_REASON } from '../app/ReasonDialog';
import { StepUpDialog } from '../app/StepUpDialog';
import { useCreditProblemText } from './labels';
import { useAdjust } from './queries';
import { CREDITS, MAX_CREDITS } from './search';

const MAX_LINK = 200;

export interface AdjustDialogProps {
  account: AdminLedgerAccount;
  onClose: () => void;
}

/**
 * Adjust a customer's wallet: whole credits added or taken away, a reason and an optional ticket or
 * order link. The server posts it at once or, above the dual-approval threshold, makes a pending
 * request for a second person (D-05); the toast says which.
 */
export function AdjustDialog({ account, onClose }: AdjustDialogProps) {
  const intl = useIntl();
  const toast = useToast();
  const id = useId();
  const problemText = useCreditProblemText();
  const adjust = useAdjust();
  const [direction, setDirection] = useState<'add' | 'remove'>('add');
  const [credits, setCredits] = useState('');
  const [reason, setReason] = useState('');
  const [link, setLink] = useState('');
  const [touched, setTouched] = useState(false);
  /** The adjustment waiting on a step-up code; sent again once the code is right. */
  const [pending, setPending] = useState<CreditAdjustmentCreate | null>(null);

  const cleanCredits = credits.trim();
  const badCredits = !CREDITS.test(cleanCredits) || Number(cleanCredits) > MAX_CREDITS;
  const cleanReason = reason.trim();
  const badReason = cleanReason.length === 0 || cleanReason.length > MAX_REASON;
  const cleanLink = link.trim();
  const name = account.ownerName ?? intl.formatMessage({ id: 'credits.noName' });
  const stepUp = adjust.error instanceof ApiError && adjust.error.code === 'step_up_required';
  const error = adjust.error && !stepUp ? problemText(adjust.error) : null;

  const send = (body: CreditAdjustmentCreate) => {
    adjust.mutate(body, {
      onSuccess: (r) => {
        const amount = money(body.amountPaise);
        toast.show({
          message:
            r.outcome === 'pending'
              ? intl.formatMessage({ id: 'credits.adjust.pendingDone' }, { amount, name })
              : intl.formatMessage({ id: 'credits.adjust.postedDone' }, { amount, name }),
        });
        onClose();
      },
      onError: (e) => {
        if (e instanceof ApiError && e.code === 'step_up_required') setPending(body);
      },
    });
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (badCredits || badReason || adjust.isPending) return;
    const paise = Number(cleanCredits) * 100;
    send({
      accountId: account.id,
      amountPaise: direction === 'add' ? paise : -paise,
      reason: cleanReason,
      ...(cleanLink ? { link: cleanLink } : {}),
    });
  };

  return (
    <>
      <Dialog
        open
        onClose={onClose}
        title={intl.formatMessage({ id: 'credits.adjust.title' }, { name })}
        description={intl.formatMessage({ id: 'credits.adjust.body' }, { balance: money(account.balancePaise) })}
        closeLabel={intl.formatMessage({ id: 'common.close' })}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {intl.formatMessage({ id: 'common.cancel' })}
            </Button>
            <Button type="submit" form={id} variant="primary" loading={adjust.isPending} loadingLabel={intl.formatMessage({ id: 'common.saving' })}>
              {intl.formatMessage({ id: 'credits.adjust.confirm' })}
            </Button>
          </>
        }
      >
        <form id={id} onSubmit={submit} noValidate>
          <div className="tz-field">
            <label className="tz-field-label" htmlFor={`${id}-direction`}>
              {intl.formatMessage({ id: 'credits.adjust.direction' })}
            </label>
            <select id={`${id}-direction`} className="tz-input" value={direction} onChange={(e) => setDirection(e.target.value === 'remove' ? 'remove' : 'add')}>
              <option value="add">{intl.formatMessage({ id: 'credits.adjust.add' })}</option>
              <option value="remove">{intl.formatMessage({ id: 'credits.adjust.remove' })}</option>
            </select>
          </div>
          <TextField
            label={intl.formatMessage({ id: 'credits.adjust.credits' })}
            value={credits}
            inputMode="numeric"
            autoComplete="off"
            maxLength={7}
            onChange={(e) => setCredits(e.target.value)}
            help={intl.formatMessage({ id: 'credits.adjust.creditsHelp' })}
            error={touched && badCredits ? intl.formatMessage({ id: 'credits.adjust.creditsInvalid' }) : undefined}
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
          <TextField
            label={intl.formatMessage({ id: 'credits.adjust.link' })}
            value={link}
            maxLength={MAX_LINK}
            autoComplete="off"
            onChange={(e) => setLink(e.target.value)}
            help={intl.formatMessage({ id: 'credits.adjust.linkHelp' })}
          />
          {error && (
            <p role="alert" className="tz-field-error">
              {error}
            </p>
          )}
        </form>
      </Dialog>
      <StepUpDialog
        open={pending !== null}
        onVerified={() => {
          if (pending) send(pending);
          setPending(null);
        }}
        onClose={() => {
          setPending(null);
          adjust.reset();
        }}
      />
    </>
  );
}
