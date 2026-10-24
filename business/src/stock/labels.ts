import type { StoreItem } from '@todayz/api-client/gen/model/storeItem';
import { formatAbsolute, formatRelative, type MessageLocale } from '@todayz/i18n';
import type { ReactNode } from 'react';
import type { IntlShape } from 'react-intl';
import type { StockLevel } from '@todayz/ui';

const DAY_MS = 24 * 60 * 60 * 1000;

/** The app's message locale from react-intl's tag: the shell passes the `<html lang>` form (`en-IN`, `kn`, `en-XA`). */
function messageLocale(intl: IntlShape): MessageLocale {
  return (intl.locale.startsWith('en-XA') ? 'en-XA' : intl.locale.split('-')[0]) as MessageLocale;
}

/** "12 min ago" within a day, then "Yesterday, 6:40 pm" and older (IST). */
export function whenText(intl: IntlShape, iso: string, now: number = Date.now()): string {
  const locale = messageLocale(intl);
  return now - Date.parse(iso) < DAY_MS ? formatRelative(iso, now, locale) : formatAbsolute(iso, now, locale);
}

/** "In stock", "Only 3 left" or "Out of stock" for the StockTag. */
export function levelLabel(intl: IntlShape, level: StockLevel, available: number): string {
  if (level === 'out') return intl.formatMessage({ id: 'stock.level.out' });
  if (level === 'low') return intl.formatMessage({ id: 'stock.level.low' }, { available });
  return intl.formatMessage({ id: 'stock.level.in' });
}

/** The freshness props of a StockTag (D-101): "Confirmed 12 min ago", or the stale line. */
export function freshnessProps(
  intl: IntlShape,
  item: Pick<StoreItem, 'stale' | 'stockUpdatedAt'>,
): { stale: true; staleLabel: ReactNode } | { stale?: false; freshness?: ReactNode } {
  if (item.stale || item.stockUpdatedAt === null) return { stale: true, staleLabel: intl.formatMessage({ id: 'stock.fresh.stale' }) };
  return { freshness: intl.formatMessage({ id: 'stock.fresh.confirmed' }, { time: whenText(intl, item.stockUpdatedAt) }) };
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'] as const;

/** "20 Nov 2026" for an expiry date: a calendar date with no time zone, so no clock arithmetic. */
export function dateText(intl: IntlShape, ymd: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(ymd);
  if (!m) return ymd;
  const [, y, mo, d] = m;
  if (intl.locale.startsWith('en')) return `${Number(d)} ${MONTHS[Number(mo) - 1] ?? mo} ${y}`;
  return new Intl.DateTimeFormat(intl.locale, { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC', numberingSystem: 'latn' }).format(
    Date.UTC(Number(y), Number(mo) - 1, Number(d)),
  );
}

export function sourceLabel(intl: IntlShape, source: string): string {
  const id = `stock.source.${source}`;
  return intl.messages[id] ? intl.formatMessage({ id }) : intl.formatMessage({ id: 'stock.source.other' });
}
