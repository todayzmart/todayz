import type { AdminNotificationChannel } from '@todayz/api-client/gen/model/adminNotificationChannel';
import type { AdminNotificationLang } from '@todayz/api-client/gen/model/adminNotificationLang';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';

/** The catalogue's kinds (internal/notify). A kind added there and not here shows its code name. */
const KIND_LABEL: Readonly<Record<string, string>> = {
  new_order: 'notifications.kind.new_order',
  delivery_return_pending: 'notifications.kind.delivery_return_pending',
  order_unseen: 'notifications.kind.order_unseen',
  new_ask: 'notifications.kind.new_ask',
  order_update: 'notifications.kind.order_update',
  ask_reply: 'notifications.kind.ask_reply',
  credits_earned: 'notifications.kind.credits_earned',
  credits_expiring: 'notifications.kind.credits_expiring',
  referral_unlocked: 'notifications.kind.referral_unlocked',
  shop_referral_unlocked: 'notifications.kind.shop_referral_unlocked',
};

const CHANNEL_LABEL: Record<AdminNotificationChannel, string> = {
  push: 'notifications.channel.push',
  sms: 'notifications.channel.sms',
  whatsapp: 'notifications.channel.whatsapp',
};

const LANG_LABEL: Record<AdminNotificationLang, string> = {
  en: 'notifications.lang.en',
  hi: 'notifications.lang.hi',
  kn: 'notifications.lang.kn',
  te: 'notifications.lang.te',
  ta: 'notifications.lang.ta',
};

export function useNotifyLabels() {
  const intl = useIntl();
  return useMemo(
    () => ({
      kind: (k: string) => (KIND_LABEL[k] ? intl.formatMessage({ id: KIND_LABEL[k] }) : k),
      channel: (c: AdminNotificationChannel) => intl.formatMessage({ id: CHANNEL_LABEL[c] }),
      lang: (l: AdminNotificationLang) => intl.formatMessage({ id: LANG_LABEL[l] }),
    }),
    [intl],
  );
}

/** A template text cut at its `{param}` placeholders, so the preview can show each one as a chip. */
export function previewParts(s: string): { text: string; param: boolean }[] {
  return s
    .split(/(\{[a-z_]+\})/)
    .filter((p) => p !== '')
    .map((p) => (/^\{[a-z_]+\}$/.test(p) ? { text: p.slice(1, -1), param: true } : { text: p, param: false }));
}
