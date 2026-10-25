import type { AdminCategory } from '@todayz/api-client/gen/model/adminCategory';
import type { AdminCategoryTint } from '@todayz/api-client/gen/model/adminCategoryTint';
import type { AdminProduct } from '@todayz/api-client/gen/model/adminProduct';
import type { AdminProductStatus } from '@todayz/api-client/gen/model/adminProductStatus';
import type { AdminProductUnit } from '@todayz/api-client/gen/model/adminProductUnit';
import type { AdminSynonymLang } from '@todayz/api-client/gen/model/adminSynonymLang';
import type { PillTone } from '@todayz/ui';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';
import { money } from '../app/format';

export const PRODUCT_STATUSES = ['active', 'pending_review', 'merged', 'retired'] as const satisfies readonly AdminProductStatus[];
export const UNITS = ['g', 'kg', 'ml', 'l', 'pc', 'pack'] as const satisfies readonly AdminProductUnit[];
export const TINTS = ['green', 'amber', 'rose', 'sky', 'lilac', 'peach'] as const satisfies readonly AdminCategoryTint[];
export const LANGS = ['en', 'hi', 'kn', 'te', 'ta'] as const satisfies readonly AdminSynonymLang[];

const TONES: Record<AdminProductStatus, PillTone> = {
  active: 'completed',
  pending_review: 'pending',
  merged: 'neutral',
  retired: 'cancelled',
};

export const productTone = (s: AdminProductStatus): PillTone => TONES[s];

export function useCatalogueLabels() {
  const intl = useIntl();
  return useMemo(() => {
    const status = (s: AdminProductStatus): string => {
      switch (s) {
        case 'active':
          return intl.formatMessage({ id: 'catalogue.status.active' });
        case 'pending_review':
          return intl.formatMessage({ id: 'catalogue.status.pending' });
        case 'merged':
          return intl.formatMessage({ id: 'catalogue.status.merged' });
        case 'retired':
          return intl.formatMessage({ id: 'catalogue.status.retired' });
      }
    };
    const lang = (l: AdminSynonymLang): string => {
      switch (l) {
        case 'en':
          return intl.formatMessage({ id: 'catalogue.lang.en' });
        case 'hi':
          return intl.formatMessage({ id: 'catalogue.lang.hi' });
        case 'kn':
          return intl.formatMessage({ id: 'catalogue.lang.kn' });
        case 'te':
          return intl.formatMessage({ id: 'catalogue.lang.te' });
        case 'ta':
          return intl.formatMessage({ id: 'catalogue.lang.ta' });
      }
    };
    return {
      status,
      lang,
      /** "Toor Dal · Unpolished", the way a store sees the row. */
      name: (p: Pick<AdminProduct, 'name' | 'variant'>): string => (p.variant ? `${p.name} · ${p.variant}` : p.name),
      /** "1 kg", "500 g": units are symbols, the same in every language. */
      pack: (p: Pick<AdminProduct, 'packSize' | 'unit'>): string => `${intl.formatNumber(p.packSize, { maximumFractionDigits: 3 })} ${p.unit}`,
      mrp: (paise: number | null): string => (paise === null ? intl.formatMessage({ id: 'catalogue.none' }) : money(paise)),
      similarity: (s: number): string => intl.formatNumber(s, { style: 'percent', maximumFractionDigits: 0 }),
      tint: (t: AdminCategoryTint): string => {
        switch (t) {
          case 'green':
            return intl.formatMessage({ id: 'catalogue.tint.green' });
          case 'amber':
            return intl.formatMessage({ id: 'catalogue.tint.amber' });
          case 'rose':
            return intl.formatMessage({ id: 'catalogue.tint.rose' });
          case 'sky':
            return intl.formatMessage({ id: 'catalogue.tint.sky' });
          case 'lilac':
            return intl.formatMessage({ id: 'catalogue.tint.lilac' });
          case 'peach':
            return intl.formatMessage({ id: 'catalogue.tint.peach' });
        }
      },
      /** A child category reads "Grocery › Festive" so the parent is never lost in a flat list. */
      category: (c: AdminCategory, all: readonly AdminCategory[]): string => {
        const parent = c.parentId ? all.find((p) => p.id === c.parentId) : undefined;
        return parent ? `${parent.names.en} › ${c.names.en}` : c.names.en;
      },
    };
  }, [intl]);
}
