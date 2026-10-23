// Category icons: the API sends a Lucide name; only the ones the catalogue uses are bundled.
import { Pen, Pill, ShoppingBag, Smartphone, Sparkles, Store, Wrench, type LucideIcon } from 'lucide-react';

const ICONS: Record<string, LucideIcon> = {
  'shopping-bag': ShoppingBag,
  pill: Pill,
  sparkles: Sparkles,
  smartphone: Smartphone,
  pen: Pen,
  wrench: Wrench,
};

export function categoryIcon(name: string): LucideIcon {
  return ICONS[name] ?? Store;
}
