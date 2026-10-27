import type { AdminSettingGroup } from '@todayz/api-client/gen/model/adminSettingGroup';
import type { AdminSettingUnit } from '@todayz/api-client/gen/model/adminSettingUnit';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';
import { money } from '../app/format';
import { MAX_TEXT, inHundredths, type ParseError } from './value';

/** Names for groups, values by unit, and edit errors. */
export function useSettingLabels() {
  const intl = useIntl();
  return useMemo(() => {
    const group = (g: AdminSettingGroup): string => {
      switch (g) {
        case 'orders':
          return intl.formatMessage({ id: 'settings.group.orders' });
        case 'stock':
          return intl.formatMessage({ id: 'settings.group.stock' });
        case 'credits':
          return intl.formatMessage({ id: 'settings.group.credits' });
        case 'reviews':
          return intl.formatMessage({ id: 'settings.group.reviews' });
        case 'referrals':
          return intl.formatMessage({ id: 'settings.group.referrals' });
        case 'pos':
          return intl.formatMessage({ id: 'settings.group.pos' });
        case 'search':
          return intl.formatMessage({ id: 'settings.group.search' });
        case 'insights':
          return intl.formatMessage({ id: 'settings.group.insights' });
        case 'field':
          return intl.formatMessage({ id: 'settings.group.field' });
        case 'jobs':
          return intl.formatMessage({ id: 'settings.group.jobs' });
        case 'asks':
          return intl.formatMessage({ id: 'settings.group.asks' });
        case 'baskets':
          return intl.formatMessage({ id: 'settings.group.baskets' });
        case 'customers':
          return intl.formatMessage({ id: 'settings.group.customers' });
        case 'access':
          return intl.formatMessage({ id: 'settings.group.access' });
        case 'risk':
          return intl.formatMessage({ id: 'settings.group.risk' });
        case 'support':
          return intl.formatMessage({ id: 'settings.group.support' });
        case 'features':
          return intl.formatMessage({ id: 'settings.group.features' });
        case 'ai':
          return intl.formatMessage({ id: 'settings.group.ai' });
        case 'clients':
          return intl.formatMessage({ id: 'settings.group.clients' });
        default:
          return intl.formatMessage({ id: 'settings.group.other' });
      }
    };

    /** A value as people read it: ₹ for paise, % for basis points, a unit word for the rest. */
    const value = (unit: AdminSettingUnit, v: unknown): string => {
      if (v === null || v === undefined) return '—';
      if (unit === 'bool') return intl.formatMessage({ id: v === true ? 'settings.value.on' : 'settings.value.off' });
      if (typeof v !== 'number') return String(v);
      switch (unit) {
        case 'paise':
          return money(v);
        case 'bp':
          return intl.formatNumber(v / 10_000, { style: 'percent', maximumFractionDigits: 2 });
        case 'credits':
          return intl.formatMessage({ id: 'settings.value.credits' }, { n: v });
        case 'minutes':
          return intl.formatMessage({ id: 'settings.value.minutes' }, { n: v });
        case 'hours':
          return intl.formatMessage({ id: 'settings.value.hours' }, { n: v });
        case 'days':
          return intl.formatMessage({ id: 'settings.value.days' }, { n: v });
        case 'meters':
          return intl.formatMessage({ id: 'settings.value.meters' }, { n: v });
        default:
          return intl.formatNumber(v);
      }
    };

    const range = (unit: AdminSettingUnit, min: number | null, max: number | null): string => {
      if (min === null && max === null) return '—';
      if (min === null) return intl.formatMessage({ id: 'settings.range.max' }, { max: value(unit, max) });
      if (max === null) return intl.formatMessage({ id: 'settings.range.min' }, { min: value(unit, min) });
      return intl.formatMessage({ id: 'settings.range.both' }, { min: value(unit, min), max: value(unit, max) });
    };

    /** What the edit field takes, under the field. */
    const inputHelp = (unit: AdminSettingUnit): string => {
      switch (unit) {
        case 'paise':
          return intl.formatMessage({ id: 'settings.edit.help.rupees' });
        case 'bp':
          return intl.formatMessage({ id: 'settings.edit.help.percent' });
        case 'text':
          return intl.formatMessage({ id: 'settings.edit.help.text' }, { max: MAX_TEXT });
        default:
          return intl.formatMessage({ id: 'settings.edit.help.whole' });
      }
    };

    const parseError = (e: ParseError, unit: AdminSettingUnit, min: number | null, max: number | null): string => {
      switch (e) {
        case 'range':
          return intl.formatMessage({ id: 'settings.edit.error.range' }, { range: range(unit, min, max) });
        case 'text':
          return intl.formatMessage({ id: 'settings.edit.error.text' }, { max: MAX_TEXT });
        case 'version':
          return intl.formatMessage({ id: 'settings.edit.error.version' });
        default:
          return inHundredths(unit)
            ? intl.formatMessage({ id: 'settings.edit.error.decimal' })
            : intl.formatMessage({ id: 'settings.edit.error.whole' });
      }
    };

    return { group, value, range, inputHelp, parseError };
  }, [intl]);
}
