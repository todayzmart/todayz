// Money, time and stock wording for discover screens. Messages take pre-formatted strings only.
import type { DiscoverSearchItem } from '@todayz/api-client/gen/model/discoverSearchItem';
import type { DiscoverUnit } from '@todayz/api-client/gen/model/discoverUnit';
import { formatMoney, formatRelative } from '@todayz/i18n';
import type { ProductCardProps } from '@todayz/ui';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';
import { useLocale } from '../app/locale';

export interface Fmt {
  money: (paise: number) => string;
  /** "12 min ago", computed against the server's clock. */
  ago: (then: string, serverNow: string) => string;
}

export function useFmt(): Fmt {
  const { locale } = useLocale();
  return useMemo(
    () => ({
      money: (paise) => formatMoney(paise),
      ago: (then, serverNow) => formatRelative(then, serverNow, locale),
    }),
    [locale],
  );
}

export function useUnitLabel(): (size: number, unit: DiscoverUnit) => string {
  const intl = useIntl();
  return (size, unit) => {
    if (unit === 'pc') return intl.formatMessage({ id: 'product.unit.pc' }, { size: String(size) });
    if (unit === 'pack') return intl.formatMessage({ id: 'product.unit.pack' }, { size: String(size) });
    return `${size} ${unit}`;
  };
}

/** Pack line under an item's name: "Fine, 5 kg" (variant first when there is one). */
export function usePackLine(): (item: DiscoverSearchItem) => string {
  const unitLabel = useUnitLabel();
  return (item) => {
    const pack = unitLabel(item.packSize, item.unit);
    return item.variant ? `${item.variant} · ${pack}` : pack;
  };
}

/** The ProductCard `stock` prop with its proof (D-101): "Confirmed 12 min ago", or the stale label. */
export function useStockProps(): (item: DiscoverSearchItem, serverNow: string) => ProductCardProps['stock'] {
  const intl = useIntl();
  const { ago } = useFmt();
  return (item, serverNow) => {
    if (!item.inStock) return { level: 'out', label: intl.formatMessage({ id: 'product.stock.out' }), freshness: null };
    const label = intl.formatMessage({ id: 'product.stock.in' });
    // Discover sends confirmedAt: null once the item is past stock_stale_after_hours: show nothing then.
    if (!item.confirmedAt) return { level: 'in', label, freshness: null };
    const age = Date.parse(serverNow) - Date.parse(item.confirmedAt);
    const freshness =
      age < 60_000
        ? intl.formatMessage({ id: 'product.confirmedNow' })
        : intl.formatMessage({ id: 'product.confirmed' }, { time: ago(item.confirmedAt, serverNow) });
    return { level: 'in', label, freshness };
  };
}

/** Whole-percent saving, or null when there is no MRP above the price. */
export function offPercent(pricePaise: number, mrpPaise: number | null): number | null {
  if (mrpPaise === null || mrpPaise <= pricePaise) return null;
  return Math.round(((mrpPaise - pricePaise) * 100) / mrpPaise);
}
