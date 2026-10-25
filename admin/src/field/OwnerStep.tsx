import { ApiError } from '@todayz/api-client';
import type { FieldBusiness } from '@todayz/api-client/gen/model/fieldBusiness';
import type { FieldOwnerCode } from '@todayz/api-client/gen/model/fieldOwnerCode';
import { Banner, Button, OtpInput, useToast } from '@todayz/ui';
import { useEffect, useState } from 'react';
import { useIntl } from 'react-intl';
import { useProblemText } from '../app/problems';
import { codeError, isOpen } from './labels';
import { useSendCode, useVerifyCode } from './queries';
import type { Step } from './steps';
import css from './field.module.css';

/** The owner's code has as many digits as a sign-in code (rules.Digits). */
const DIGITS = 6;

/** Which step fixes a field the API says is missing before the owner can confirm. */
function stepOf(field: string): Step {
  return field === 'categoryId' ? 'details' : 'store';
}

/** Seconds until another code may be sent, counting down once a second. */
function useCountdown(): [number, (s: number) => void] {
  const [left, setLeft] = useState(0);
  useEffect(() => {
    if (left <= 0) return;
    const t = window.setTimeout(() => setLeft((n) => n - 1), 1000);
    return () => window.clearTimeout(t);
  }, [left]);
  return [left, setLeft];
}

/**
 * Step 5: the owner reads out the code sent to their number. The right code makes that number's
 * account the owner and sends the business for verification; the number itself is never shown.
 */
export function OwnerStep({ business: b, goTo }: { business: FieldBusiness; goTo: (s: Step) => void }) {
  const intl = useIntl();
  const toast = useToast();
  const problemText = useProblemText();
  const send = useSendCode(b.id);
  const verify = useVerifyCode(b.id);
  const [sent, setSent] = useState<FieldOwnerCode | null>(null);
  const [code, setCode] = useState('');
  const [tries, setTries] = useState(0);
  const [wait, setWait] = useCountdown();
  const t = (id: string, v?: Record<string, string | number>) => intl.formatMessage({ id }, v);

  if (b.ownerConfirmed)
    return (
      <Banner tone="info" title={t('field.owner.done.title')}>
        {t('field.owner.done.body')}
      </Banner>
    );
  if (!isOpen(b.status)) return <p className={css.muted}>{t('field.owner.notOpen')}</p>;

  const sendCode = () => {
    verify.reset();
    setCode('');
    send.mutate(undefined, {
      onSuccess: (c) => {
        setSent(c);
        setWait(c.resendAfterSeconds);
      },
      onError: (err) => {
        const e = codeError(err);
        if (e.kind === 'wait' && e.retryAfter) setWait(e.retryAfter);
      },
    });
  };

  const check = (value: string) => {
    if (verify.isPending) return;
    verify.mutate(value, {
      onSuccess: () => toast.show({ message: t('field.owner.confirmed') }),
      onError: () => {
        setTries((n) => n + 1);
        setCode('');
      },
    });
  };

  const missing = send.error instanceof ApiError && send.error.code === 'submit_incomplete' ? Object.keys(send.error.fields) : [];
  const fix = missing.length > 0 ? stepOf(missing[0] ?? '') : null;
  const sendError = (() => {
    if (!send.isError || fix) return null;
    const e = codeError(send.error);
    if (e.kind === 'wait') return e.retryAfter ? t('field.owner.wait', { s: e.retryAfter }) : t('field.owner.waitShort');
    if (e.kind === 'gone') return t('field.owner.gone');
    return problemText(send.error);
  })();
  const verifyError = (() => {
    if (!verify.isError) return null;
    const e = codeError(verify.error);
    switch (e.kind) {
      case 'wrong':
        return e.attemptsLeft !== undefined ? t('field.owner.wrong.left', { n: e.attemptsLeft }) : t('field.owner.wrong');
      case 'locked':
        return t('field.owner.locked');
      case 'expired':
        return t('field.owner.expired');
      case 'wait':
        return t('field.owner.waitShort');
      case 'gone':
        return t('field.owner.gone');
      default:
        return verify.error instanceof ApiError && verify.error.code === 'submit_incomplete' ? t('field.owner.incomplete') : problemText(verify.error);
    }
  })();
  const needNew = verify.isError && ['locked', 'expired'].includes(codeError(verify.error).kind);

  return (
    <div className={css.form}>
      <p className={css.muted}>{t('field.owner.help')}</p>
      {fix && (
        <Banner tone="warning" title={t('field.owner.incomplete')}>
          <Button size="sm" variant="soft" onClick={() => goTo(fix)}>
            {t(fix === 'details' ? 'field.owner.fixDetails' : 'field.owner.fixStore')}
          </Button>
        </Banner>
      )}
      {sendError && (
        <p role="alert" className="tz-field-error">
          {sendError}
        </p>
      )}
      {sent ? (
        <>
          <p>{t('field.owner.sent', { masked: sent.phoneMasked, until: intl.formatTime(sent.expiresAt, { timeStyle: 'short', timeZone: 'Asia/Kolkata' }) })}</p>
          <OtpInput
            className={css.code}
            label={t('field.owner.code')}
            digitLabel={(i) => t('field.owner.digit', { i })}
            length={DIGITS}
            value={code}
            onChange={setCode}
            onComplete={check}
            error={verifyError ?? undefined}
            errorKey={tries}
            disabled={verify.isPending || needNew}
          />
          <Button variant="primary" block loading={verify.isPending} disabled={code.length !== DIGITS || needNew} onClick={() => check(code)}>
            {t('field.owner.confirm')}
          </Button>
          <Button variant="ghost" loading={send.isPending} disabled={wait > 0} onClick={sendCode}>
            {wait > 0 ? t('field.owner.resendIn', { s: wait }) : t('field.owner.resend')}
          </Button>
        </>
      ) : (
        <Button variant="primary" block loading={send.isPending} disabled={wait > 0} onClick={sendCode}>
          {wait > 0 ? t('field.owner.resendIn', { s: wait }) : t('field.owner.send')}
        </Button>
      )}
    </div>
  );
}
