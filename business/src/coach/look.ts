import type { CoachAction } from '@todayz/api-client/gen/model/coachAction';
import type { CoachCardKind } from '@todayz/api-client/gen/model/coachCardKind';
import type { InsightAction } from '@todayz/api-client/gen/model/insightAction';
import type { Tint } from '@todayz/ui';
import { Lightbulb, ListChecks, TrendingDown, TrendingUp, type LucideIcon } from 'lucide-react';
import type { IntlShape } from 'react-intl';
import { actionHref, actionLabel } from '../overview/insightRows';

/** The card type's tint and icon (06 B-19: High, Low, Do this, Tip). */
export const KIND_LOOK: Record<CoachCardKind, { tint: Tint; icon: LucideIcon }> = {
  high: { tint: 'green', icon: TrendingUp },
  low: { tint: 'rose', icon: TrendingDown },
  action: { tint: 'amber', icon: ListChecks },
  tip: { tint: 'sky', icon: Lightbulb },
};

export function kindLabel(intl: IntlShape, kind: CoachCardKind): string {
  return intl.formatMessage({ id: `coach.kind.${kind}` });
}

/** The Demand insights actions share their deep links and labels with the Overview card. */
const INSIGHT_ACTIONS = new Set<string>([
  'restock',
  'create_offer',
  'flash_offer',
  'add_product',
  'mark_expired',
  'edit_price',
  'check_cost',
  'count_stock',
  'see_customers',
]);

const asInsight = (a: CoachAction): InsightAction | null => (INSIGHT_ACTIONS.has(a.type) ? (a as InsightAction) : null);

/**
 * Where the card's button goes, prefilled; null when the card has no button. It opens a screen and
 * applies nothing (the person confirms there).
 */
export function coachHref(a: CoachAction): string | null {
  const insight = asInsight(a);
  if (insight) return actionHref(insight);
  switch (a.type) {
    case 'reply_reviews':
      return '/reviews';
    case 'turn_on_auto_accept':
    case 'enable_delivery':
      return '/settings';
    case 'update_stock':
      return '/stock';
    default:
      return null;
  }
}

export function coachActionLabel(intl: IntlShape, a: CoachAction): string {
  const insight = asInsight(a);
  if (insight) return actionLabel(intl, insight);
  return intl.formatMessage({ id: `coach.action.${a.type}` });
}
