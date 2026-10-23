import { formatDistance } from '@todayz/i18n';
import type { InsightRow } from '@todayz/ui';
import type { ReactNode } from 'react';
import type { IntlShape } from 'react-intl';
import { weekday } from './format';
import type { InsightData } from './types';

const strong = (chunks: ReactNode[]) => <strong>{chunks}</strong>;

const q = (params: Record<string, string>) => new URLSearchParams(params).toString();

/**
 * One kit row per insight. Each action is the deep link of 06-screens "Insight deep links": it
 * opens the target screen prefilled and applies nothing.
 */
export function toRow(intl: IntlShape, d: InsightData): InsightRow {
  switch (d.kind) {
    case 'selling_fast':
      return {
        id: d.id,
        kind: d.kind,
        label: intl.formatMessage({ id: 'overview.insights.selling_fast.label' }),
        sentence: intl.formatMessage(
          { id: 'overview.insights.selling_fast.sentence' },
          { item: d.item, sold: d.sold, day: weekday(intl, d.runOutDay), strong },
        ),
        action: {
          label: intl.formatMessage({ id: 'overview.insights.selling_fast.action' }),
          href: `/stock?${q({ restock: d.storeItemId, qty: String(d.restockQty) })}`,
        },
      };
    case 'going_to_waste':
      return {
        id: d.id,
        kind: d.kind,
        label: intl.formatMessage({ id: 'overview.insights.going_to_waste.label' }),
        sentence: intl.formatMessage(
          { id: 'overview.insights.going_to_waste.sentence' },
          { count: d.count, days: d.days, items: intl.formatList(d.items, { type: 'unit' }), strong },
        ),
        action: {
          label: intl.formatMessage({ id: 'overview.insights.going_to_waste.action' }),
          href: `/offers?${q({ offer: 'flash', batches: d.batchIds.join(',') })}`,
        },
      };
    case 'sitting_on_shelf':
      return {
        id: d.id,
        kind: d.kind,
        label: intl.formatMessage({ id: 'overview.insights.sitting_on_shelf.label' }),
        sentence: intl.formatMessage(
          { id: 'overview.insights.sitting_on_shelf.sentence' },
          { item: d.item, stock: d.inStock, days: d.daysSinceSale, strong },
        ),
        action: {
          label: intl.formatMessage({ id: 'overview.insights.sitting_on_shelf.action' }),
          href: `/offers?${q({ offer: 'item', item: d.storeItemId, pct: String(d.pct) })}`,
        },
      };
    case 'wanted_nearby': {
      const values = { item: d.item, searches: d.searches, distance: formatDistance(d.radiusMetres), strong };
      return {
        id: d.id,
        kind: d.kind,
        label: intl.formatMessage({ id: 'overview.insights.wanted_nearby.label' }),
        sentence: d.noShopNearby
          ? intl.formatMessage({ id: 'overview.insights.wanted_nearby.sentenceNone' }, values)
          : intl.formatMessage({ id: 'overview.insights.wanted_nearby.sentence' }, values),
        action: {
          label: intl.formatMessage({ id: 'overview.insights.wanted_nearby.action' }),
          href: `/stock?${q({ add: d.masterProductId })}`,
        },
      };
    }
    case 'bought_together':
      return {
        id: d.id,
        kind: d.kind,
        label: intl.formatMessage({ id: 'overview.insights.bought_together.label' }),
        sentence: intl.formatMessage(
          { id: 'overview.insights.bought_together.sentence' },
          { a: d.itemA, b: d.itemB, pct: d.sharePct, strong },
        ),
        action: {
          label: intl.formatMessage({ id: 'overview.insights.bought_together.action' }),
          href: `/offers?${q({ offer: 'pair', items: d.storeItemIds.join(',') })}`,
        },
      };
  }
}
