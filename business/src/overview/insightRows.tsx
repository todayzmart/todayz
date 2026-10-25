import type { InsightAction } from '@todayz/api-client/gen/model/insightAction';
import { formatDistance, formatMoney } from '@todayz/i18n';
import type { InsightRow } from '@todayz/ui';
import type { ReactNode } from 'react';
import type { IntlShape } from 'react-intl';
import { InsightText } from '../insights/text/InsightText';
import { weekday } from './format';
import type { InsightData } from './types';

const strong = (chunks: ReactNode[]) => <strong>{chunks}</strong>;

const q = (params: Record<string, string>) => new URLSearchParams(params).toString();

type Bag = Readonly<Record<string, unknown>>;

const num = (b: Bag | null | undefined, k: string): number => {
  const v = b?.[k];
  return typeof v === 'number' && Number.isFinite(v) ? v : 0;
};
const str = (b: Bag | null | undefined, k: string): string => {
  const v = b?.[k];
  return typeof v === 'string' ? v : '';
};
const list = (b: Bag | null | undefined, k: string): string[] => {
  const v = b?.[k];
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [];
};

/** "Amul Taaza 500 ml": the item name with its pack size. */
const named = (name: string, size: string) => (size ? `${name} ${size}` : name);

/** An IST calendar day ("2026-01-10") as an instant at IST noon, so the weekday never slips a day. */
const istDay = (day: string) => `${day}T12:00:00+05:30`;

/** "Saturday" within the week, "3 Nov" further out (01 §12 Selling fast). */
function runOutDay(intl: IntlShape, day: string, inDays: number): string {
  if (!day) return '';
  return inDays < 7
    ? weekday(intl, istDay(day))
    : intl.formatDate(new Date(istDay(day)), { day: 'numeric', month: 'short', timeZone: 'Asia/Kolkata' });
}

/** Most ids a deep link carries (06 "Insight deep links": count at most 100). */
const MAX_LINK_IDS = 100;

/**
 * The deep link of 06-screens "Insight deep links" for an action. It opens the target screen
 * prefilled and applies nothing; the person confirms there.
 */
export function actionHref(action: InsightAction): string {
  const p = action.payload as Bag;
  switch (action.type) {
    case 'restock':
      return `/stock?${q({ restock: str(p, 'storeItemId'), qty: String(num(p, 'qty')) })}`;
    case 'create_offer': {
      const pair = list(p, 'storeItemIds');
      if (pair.length > 0) return `/offers?${q({ offer: 'pair', items: pair.join(',') })}`;
      const pct = num(p, 'pct');
      return `/offers?${q({ offer: 'item', item: str(p, 'storeItemId'), ...(pct > 0 ? { pct: String(pct) } : {}) })}`;
    }
    case 'flash_offer':
      return `/offers?${q({ offer: 'flash', batches: list(p, 'batchIds').join(',') })}`;
    case 'add_product':
      return str(p, 'productId') ? `/stock?${q({ add: str(p, 'productId') })}` : `/stock?${q({ add_q: str(p, 'query') })}`;
    case 'mark_expired':
      return `/stock?${q({ expire: str(p, 'storeItemId'), batches: list(p, 'batchIds').join(',') })}`;
    case 'edit_price':
      return `/stock?${q({ price: str(p, 'storeItemId'), ref: String(num(p, 'refPaise')) })}`;
    case 'check_cost':
      return `/stock?${q({ batch: str(p, 'batchId'), item: str(p, 'storeItemId') })}`;
    case 'count_stock':
      return `/stock?${q({ count: list(p, 'storeItemIds').slice(0, MAX_LINK_IDS).join(',') })}`;
    case 'see_customers':
      return `/customers?${q({ segment: str(p, 'segment') || 'slipping' })}`;
  }
}

export function actionLabel(intl: IntlShape, action: InsightAction): string {
  switch (action.type) {
    case 'restock':
      return intl.formatMessage({ id: 'overview.insights.action.restock' });
    case 'create_offer':
      return intl.formatMessage({ id: 'overview.insights.action.create_offer' });
    case 'flash_offer':
      return intl.formatMessage({ id: 'overview.insights.action.flash_offer' });
    case 'add_product':
      return intl.formatMessage({ id: 'overview.insights.action.add_product' });
    case 'mark_expired':
      return intl.formatMessage({ id: 'overview.insights.action.mark_expired' });
    case 'edit_price':
      return intl.formatMessage({ id: 'overview.insights.action.edit_price' });
    case 'check_cost':
      return intl.formatMessage({ id: 'overview.insights.action.check_cost' });
    case 'count_stock':
      return intl.formatMessage({ id: 'overview.insights.action.count_stock' });
    case 'see_customers':
      return intl.formatMessage({ id: 'overview.insights.action.see_customers' });
  }
}

export function kindLabel(intl: IntlShape, kind: InsightData['kind']): string {
  switch (kind) {
    case 'selling_fast':
      return intl.formatMessage({ id: 'overview.insights.selling_fast.label' });
    case 'sitting_on_shelf':
      return intl.formatMessage({ id: 'overview.insights.sitting_on_shelf.label' });
    case 'going_to_waste':
      return intl.formatMessage({ id: 'overview.insights.going_to_waste.label' });
    case 'wanted_nearby':
      return intl.formatMessage({ id: 'overview.insights.wanted_nearby.label' });
    case 'missed_sales':
      return intl.formatMessage({ id: 'overview.insights.missed_sales.label' });
    case 'expired_on_shelf':
      return intl.formatMessage({ id: 'overview.insights.expired_on_shelf.label' });
    case 'selling_below_cost':
      return intl.formatMessage({ id: 'overview.insights.selling_below_cost.label' });
    case 'stock_check_due':
      return intl.formatMessage({ id: 'overview.insights.stock_check_due.label' });
    case 'bought_together':
      return intl.formatMessage({ id: 'overview.insights.bought_together.label' });
    case 'regulars_slipping':
      return intl.formatMessage({ id: 'overview.insights.regulars_slipping.label' });
    case 'priced_above_nearby':
      return intl.formatMessage({ id: 'overview.insights.priced_above_nearby.label' });
  }
}

/** The 01 §12 sentence of a row, from its facts. An AI-R4 sentence (W3-11) replaces it when present. */
export function sentence(intl: IntlShape, d: InsightData): ReactNode {
  if (d.text) return <InsightText text={d.text} />;
  const f = d.facts as Bag | null;
  const item = named(str(f, 'item'), str(f, 'size'));
  switch (d.kind) {
    case 'selling_fast': {
      const inDays = num(f, 'runOutInDays');
      return intl.formatMessage(
        { id: 'overview.insights.selling_fast.sentence' },
        { item, sold: num(f, 'soldQty'), period: num(f, 'periodDays'), inDays, day: runOutDay(intl, str(f, 'runOutOn'), inDays), strong },
      );
    }
    case 'sitting_on_shelf': {
      const values = { item, stock: num(f, 'onHand'), days: num(f, 'daysNoSale'), strong };
      return f && typeof f.valueAtCostPaise === 'number'
        ? intl.formatMessage(
            { id: 'overview.insights.sitting_on_shelf.sentenceCost' },
            { ...values, value: formatMoney(num(f, 'valueAtCostPaise'), 'dashboard') },
          )
        : intl.formatMessage({ id: 'overview.insights.sitting_on_shelf.sentence' }, values);
    }
    case 'going_to_waste':
      return intl.formatMessage(
        { id: 'overview.insights.going_to_waste.sentence' },
        {
          count: num(f, 'expiringCount'),
          days: num(f, 'withinDays'),
          items: intl.formatList(list(f, 'names'), { type: 'unit' }),
          more: num(f, 'moreCount'),
          strong,
        },
      );
    case 'wanted_nearby': {
      const values = {
        item,
        searches: num(f, 'searches'),
        distance: formatDistance(num(f, 'radiusM')),
        period: num(f, 'periodDays'),
        strong,
      };
      return f?.noShopNearby === true
        ? intl.formatMessage({ id: 'overview.insights.wanted_nearby.sentenceNone' }, values)
        : intl.formatMessage({ id: 'overview.insights.wanted_nearby.sentence' }, values);
    }
    case 'missed_sales':
      return intl.formatMessage(
        { id: 'overview.insights.missed_sales.sentence' },
        { item, days: num(f, 'zeroDays'), searches: num(f, 'searches'), strong },
      );
    case 'expired_on_shelf':
      return intl.formatMessage(
        { id: 'overview.insights.expired_on_shelf.sentence' },
        { item, qty: num(f, 'qty'), ago: num(f, 'expiredDaysAgo'), strong },
      );
    case 'selling_below_cost':
      return f?.checkCost === true
        ? intl.formatMessage({ id: 'overview.insights.selling_below_cost.sentenceCheck' }, { item, strong })
        : intl.formatMessage(
            { id: 'overview.insights.selling_below_cost.sentence' },
            { item, price: formatMoney(num(f, 'pricePaise'), 'dashboard'), cost: formatMoney(num(f, 'costPaise'), 'dashboard'), strong },
          );
    case 'stock_check_due': {
      const hours = num(f, 'staleAfterHours');
      const byDay = hours > 0 && hours % 24 === 0;
      return intl.formatMessage(
        { id: 'overview.insights.stock_check_due.sentence' },
        { count: num(f, 'staleCount'), unit: byDay ? 'days' : 'hours', span: byDay ? hours / 24 : hours, strong },
      );
    }
    case 'bought_together':
      return intl.formatMessage(
        { id: 'overview.insights.bought_together.sentence' },
        { a: named(str(f, 'itemA'), str(f, 'sizeA')), b: named(str(f, 'itemB'), str(f, 'sizeB')), oneIn: num(f, 'oneIn'), strong },
      );
    case 'regulars_slipping': {
      const days = num(f, 'lapseDays');
      const byWeek = days > 0 && days % 7 === 0;
      return intl.formatMessage(
        { id: 'overview.insights.regulars_slipping.sentence' },
        { count: num(f, 'count'), unit: byWeek ? 'weeks' : 'days', span: byWeek ? days / 7 : days, strong },
      );
    }
    case 'priced_above_nearby':
      return intl.formatMessage(
        { id: 'overview.insights.priced_above_nearby.sentence' },
        { item, price: formatMoney(num(f, 'pricePaise'), 'dashboard'), typical: formatMoney(num(f, 'typicalPaise'), 'dashboard'), strong },
      );
  }
}

/** One kit row per firing insight; a row without an action (none today) is left out of the card. */
export function toRow(intl: IntlShape, d: InsightData): InsightRow | null {
  if (!d.action || d.evidence.state !== 'ready') return null;
  return {
    id: d.id,
    kind: d.kind,
    label: kindLabel(intl, d.kind),
    sentence: sentence(intl, d),
    action: { label: actionLabel(intl, d.action), href: actionHref(d.action) },
  };
}
