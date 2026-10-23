import {
  CalendarX,
  CircleDollarSign,
  ClipboardCheck,
  Hourglass,
  PackageX,
  Search,
  ShoppingCart,
  Tag,
  TrendingUp,
  TriangleAlert,
  UserRoundMinus,
} from 'lucide-react';
import type { ReactNode } from 'react';
import type { LucideIcon } from '../internal/Icon';
import type { Tint } from '../StoreCard/StoreCard';

/** The 11 insight kinds of D-99 (01-product-rules §12), in table order. */
export type InsightKind =
  | 'selling_fast'
  | 'sitting_on_shelf'
  | 'going_to_waste'
  | 'wanted_nearby'
  | 'missed_sales'
  | 'expired_on_shelf'
  | 'selling_below_cost'
  | 'stock_check_due'
  | 'bought_together'
  | 'regulars_slipping'
  | 'priced_above_nearby';

/** Tint and icon per kind. The design fixes the first four; the rest follow the same palette logic. */
export const INSIGHT_LOOK: Record<InsightKind, { tint: Tint; icon: LucideIcon }> = {
  selling_fast: { tint: 'green', icon: TrendingUp },
  sitting_on_shelf: { tint: 'amber', icon: Hourglass },
  going_to_waste: { tint: 'rose', icon: TriangleAlert },
  wanted_nearby: { tint: 'sky', icon: Search },
  missed_sales: { tint: 'peach', icon: PackageX },
  expired_on_shelf: { tint: 'rose', icon: CalendarX },
  selling_below_cost: { tint: 'rose', icon: CircleDollarSign },
  stock_check_due: { tint: 'lilac', icon: ClipboardCheck },
  bought_together: { tint: 'lilac', icon: ShoppingCart },
  regulars_slipping: { tint: 'peach', icon: UserRoundMinus },
  priced_above_nearby: { tint: 'sky', icon: Tag },
};

/** Kinds that may be dismissed. Expired on shelf is a food-safety row and never offers "Not useful". */
export function canDismissInsight(kind: InsightKind): boolean {
  return kind !== 'expired_on_shelf';
}

export interface InsightRow {
  /** Stable id of the row (kind + subject). */
  id: string;
  kind: InsightKind;
  /** e.g. "Selling fast". */
  label: ReactNode;
  /** One sentence with the product name in `<strong>`, pre-formatted by the app. */
  sentence: ReactNode;
  /** Text action, e.g. "Restock". The arrow is added. It opens a prefilled screen and applies nothing. */
  action: { label: string; href: string };
}

/** First row per kind, in the order given, at most `limit`. */
export function pickInsightRows(rows: readonly InsightRow[], limit: number): InsightRow[] {
  const seen = new Set<InsightKind>();
  const out: InsightRow[] = [];
  for (const r of rows) {
    if (out.length >= limit) break;
    if (seen.has(r.kind)) continue;
    seen.add(r.kind);
    out.push(r);
  }
  return out;
}
