import {
  ChartColumn,
  ChartLine,
  Ellipsis,
  Package,
  ReceiptText,
  ScanBarcode,
  Settings,
  Tag,
  UsersRound,
  type LucideIcon,
} from 'lucide-react';

export interface NavItem {
  /** URL segment. `/` redirects to `/orders` until W3-03 builds Overview (D-84). */
  section: string;
  /** i18n key in shared/i18n/messages/business/shell.en.json */
  label: string;
  icon: LucideIcon;
  /** Phone bottom tabs (below 1024px). */
  tab: boolean;
  /** Desktop sidebar group: null for the top block. */
  group: 'shell.nav.groupGrow' | 'shell.nav.groupShop' | null;
  /** Shown in the sidebar (More is phone-only; its items are in the sidebar already). */
  side: boolean;
}

/** 06-screens.md "Business global": phone tabs Overview, Orders, Sell, Stock, More; desktop sidebar. */
export const NAV: readonly NavItem[] = [
  { section: 'overview', label: 'shell.nav.overview', icon: ChartColumn, tab: true, side: true, group: null },
  { section: 'orders', label: 'shell.nav.orders', icon: ReceiptText, tab: true, side: true, group: null },
  { section: 'sell', label: 'shell.nav.sell', icon: ScanBarcode, tab: true, side: true, group: null },
  { section: 'stock', label: 'shell.nav.stock', icon: Package, tab: true, side: true, group: null },
  { section: 'customers', label: 'shell.nav.customers', icon: UsersRound, tab: false, side: true, group: null },
  { section: 'offers', label: 'shell.nav.offers', icon: Tag, tab: false, side: true, group: 'shell.nav.groupGrow' },
  { section: 'analytics', label: 'shell.nav.analytics', icon: ChartLine, tab: false, side: true, group: 'shell.nav.groupGrow' },
  { section: 'settings', label: 'shell.nav.settings', icon: Settings, tab: false, side: true, group: 'shell.nav.groupShop' },
  { section: 'more', label: 'shell.nav.more', icon: Ellipsis, tab: true, side: false, group: null },
];

export const TABS = NAV.filter((n) => n.tab);
export const SIDE = NAV.filter((n) => n.side);
/** Items the More tab lists on phone: everything in the sidebar that is not a tab. */
export const MORE = NAV.filter((n) => n.side && !n.tab);

/**
 * Sections `/$section` answers with a placeholder; anything else is a 404. A WP that builds a
 * section adds its static route file (`src/routes/<area>/…`), which outranks `/$section`, and
 * changes nothing here.
 */
export const PLACEHOLDER_SECTIONS: readonly string[] = NAV.map((n) => n.section);

export function navItem(section: string): NavItem | undefined {
  return NAV.find((n) => n.section === section);
}
