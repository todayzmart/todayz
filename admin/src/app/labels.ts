// Strings the shared auth pieces take as props. Every id is a literal so the message lint sees it.
import type { PhoneOtpLabels, UpdateLabels } from '@todayz/auth';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';

export function usePhoneOtpLabels(): PhoneOtpLabels {
  const intl = useIntl();
  return useMemo(
    () => ({
      phoneLabel: intl.formatMessage({ id: 'auth.phone.label' }),
      phoneHelp: intl.formatMessage({ id: 'auth.phone.help' }),
      sendCode: intl.formatMessage({ id: 'auth.code.send' }),
      sendingCode: intl.formatMessage({ id: 'auth.code.sending' }),
      codeLabel: intl.formatMessage({ id: 'auth.code.label' }),
      digitLabel: (position: number) => intl.formatMessage({ id: 'auth.code.digit' }, { position }),
      verifying: intl.formatMessage({ id: 'auth.code.verifying' }),
      resend: intl.formatMessage({ id: 'auth.code.resend' }),
      resendIn: (seconds: number) => intl.formatMessage({ id: 'auth.code.resendIn' }, { seconds }),
      changeNumber: intl.formatMessage({ id: 'auth.code.change' }),
      invalidPhone: intl.formatMessage({ id: 'auth.phone.invalid' }),
      sentTo: (phone: string) => intl.formatMessage({ id: 'auth.code.sentTo' }, { phone }),
      recaptchaNotice: intl.formatMessage({ id: 'auth.firebase.recaptchaNotice' }),
      languageCode: intl.locale,
      errors: {
        wrongCode: intl.formatMessage({ id: 'auth.error.wrongCode' }),
        expired: intl.formatMessage({ id: 'auth.error.expired' }),
        locked: intl.formatMessage({ id: 'auth.error.locked' }),
        signinLocked: intl.formatMessage({ id: 'auth.error.signinLocked' }),
        rateLimited: intl.formatMessage({ id: 'auth.error.rateLimited' }),
        blocked: intl.formatMessage({ id: 'auth.error.blocked' }),
        network: intl.formatMessage({ id: 'auth.error.network' }),
        generic: intl.formatMessage({ id: 'auth.error.generic' }),
        captcha: intl.formatMessage({ id: 'auth.error.captcha' }),
        methodChanged: intl.formatMessage({ id: 'auth.error.methodChanged' }),
      },
    }),
    [intl],
  );
}

export function useUpdateLabels(): UpdateLabels {
  const intl = useIntl();
  return useMemo(
    () => ({
      message: intl.formatMessage({ id: 'app.update.message' }),
      action: intl.formatMessage({ id: 'app.update.action' }),
    }),
    [intl],
  );
}
