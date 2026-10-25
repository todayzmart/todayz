import type { AdminNotificationApp } from '@todayz/api-client/gen/model/adminNotificationApp';
import type { AdminNotificationLang } from '@todayz/api-client/gen/model/adminNotificationLang';
import { text } from '../app/search';

export const LANGS: readonly AdminNotificationLang[] = ['en', 'hi', 'kn', 'te', 'ta'];

/** A-16 view, kept in the URL: whose kinds, the kind open beside the list, and its language. */
export interface NotificationsSearch {
  /** Absent is every kind. */
  app?: AdminNotificationApp;
  kind?: string;
  /** Absent is English. */
  lang?: Exclude<AdminNotificationLang, 'en'>;
}

const KIND = /^[a-z][a-z_]{0,31}$/;

/** Unknown or malformed parameters are dropped, never sent to the API. */
export function validateNotificationsSearch(raw: Record<string, unknown>): NotificationsSearch {
  const out: NotificationsSearch = {};
  const app = text(raw.app);
  if (app === 'customer' || app === 'business') out.app = app;
  const kind = text(raw.kind);
  if (KIND.test(kind)) out.kind = kind;
  const lang = text(raw.lang);
  if (lang !== 'en' && (LANGS as readonly string[]).includes(lang)) out.lang = lang as Exclude<AdminNotificationLang, 'en'>;
  return out;
}
