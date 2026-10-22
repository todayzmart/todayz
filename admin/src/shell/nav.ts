import {
  Activity,
  Banknote,
  Gift,
  LifeBuoy,
  MapPin,
  Package,
  ReceiptText,
  Settings,
  ShieldCheck,
  Store,
  UsersRound,
  type LucideIcon,
} from 'lucide-react';

export interface NavItem {
  /** URL segment; '' is Pulse (A-02). */
  section: string;
  /** i18n key in shared/i18n/messages/admin/shell.en.json */
  label: string;
  icon: LucideIcon;
  group: 'shell.nav.groupGrowth' | 'shell.nav.groupAccess' | null;
}

/**
 * Sidebar from the AdminConsole design, named after 06-screens.md (A-xx). Role filtering (D-64)
 * comes with sign-in; until then every item shows.
 */
export const NAV: readonly NavItem[] = [
  { section: '', label: 'shell.nav.pulse', icon: Activity, group: null },
  { section: 'businesses', label: 'shell.nav.businesses', icon: Store, group: null },
  { section: 'stores', label: 'shell.nav.stores', icon: MapPin, group: null },
  { section: 'customers', label: 'shell.nav.customers', icon: UsersRound, group: null },
  { section: 'orders', label: 'shell.nav.orders', icon: ReceiptText, group: null },
  { section: 'support', label: 'shell.nav.support', icon: LifeBuoy, group: null },
  { section: 'credits', label: 'shell.nav.credits', icon: Gift, group: 'shell.nav.groupGrowth' },
  { section: 'catalogue', label: 'shell.nav.catalogue', icon: Package, group: 'shell.nav.groupGrowth' },
  { section: 'payouts', label: 'shell.nav.payouts', icon: Banknote, group: 'shell.nav.groupGrowth' },
  { section: 'admin-users', label: 'shell.nav.adminUsers', icon: ShieldCheck, group: 'shell.nav.groupAccess' },
  { section: 'settings', label: 'shell.nav.settings', icon: Settings, group: 'shell.nav.groupAccess' },
];

/**
 * Sections `$section` renders a placeholder for. A WP builds its section by adding a static route
 * file (`src/routes/<area>/…`), which outranks `$section`; nothing here changes (src/app/routing.test.ts).
 */
export const PLACEHOLDER_SECTIONS: readonly string[] = NAV.map((n) => n.section).filter(Boolean);

export function navItem(section: string): NavItem | undefined {
  return NAV.find((n) => n.section === section);
}
