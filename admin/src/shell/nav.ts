import {
  Activity,
  Banknote,
  Bell,
  CalendarClock,
  Gift,
  Image,
  LifeBuoy,
  MapPin,
  Package,
  ReceiptText,
  ScrollText,
  Server,
  Settings,
  ShieldCheck,
  Star,
  Store,
  TrendingUp,
  UsersRound,
  type LucideIcon,
} from 'lucide-react';

export interface NavItem {
  /** URL segment; '' is Pulse (A-02). */
  section: string;
  /** i18n key in shared/i18n/messages/admin/shell.en.json */
  label: string;
  icon: LucideIcon;
  group: 'shell.nav.groupGrowth' | 'shell.nav.groupAccess' | 'shell.nav.groupSystem' | null;
}

/**
 * Sidebar from the AdminConsole design, named after 06-screens.md (A-xx). The Shell shows only the
 * items the caller's roles open (`app/nav/access.ts`, D-64).
 */
export const NAV: readonly NavItem[] = [
  { section: '', label: 'shell.nav.pulse', icon: Activity, group: null },
  { section: 'businesses', label: 'shell.nav.businesses', icon: Store, group: null },
  { section: 'stores', label: 'shell.nav.stores', icon: MapPin, group: null },
  { section: 'customers', label: 'shell.nav.customers', icon: UsersRound, group: null },
  { section: 'orders', label: 'shell.nav.orders', icon: ReceiptText, group: null },
  { section: 'support', label: 'shell.nav.support', icon: LifeBuoy, group: null },
  { section: 'reviews', label: 'shell.nav.reviews', icon: Star, group: null },
  { section: 'banners', label: 'shell.nav.banners', icon: Image, group: null },
  { section: 'credits', label: 'shell.nav.credits', icon: Gift, group: 'shell.nav.groupGrowth' },
  { section: 'catalogue', label: 'shell.nav.catalogue', icon: Package, group: 'shell.nav.groupGrowth' },
  { section: 'demand', label: 'shell.nav.demand', icon: TrendingUp, group: 'shell.nav.groupGrowth' },
  { section: 'payouts', label: 'shell.nav.payouts', icon: Banknote, group: 'shell.nav.groupGrowth' },
  { section: 'admin-users', label: 'shell.nav.adminUsers', icon: ShieldCheck, group: 'shell.nav.groupAccess' },
  { section: 'settings', label: 'shell.nav.settings', icon: Settings, group: 'shell.nav.groupAccess' },
  { section: 'audit', label: 'shell.nav.audit', icon: ScrollText, group: 'shell.nav.groupAccess' },
  { section: 'notifications', label: 'shell.nav.notifications', icon: Bell, group: 'shell.nav.groupSystem' },
  { section: 'jobs', label: 'shell.nav.jobs', icon: CalendarClock, group: 'shell.nav.groupSystem' },
  { section: 'system', label: 'shell.nav.system', icon: Server, group: 'shell.nav.groupSystem' },
];

/**
 * Sections `$section` renders a placeholder for. A WP builds its section by adding a static route
 * file (`src/routes/<area>/…`), which outranks `$section`; nothing here changes (src/app/routing.test.ts).
 */
/** Sections with a real route (src/routes/<area>/); the rest show the placeholder. */
export const BUILT_SECTIONS: readonly string[] = ['customers', 'orders', 'reviews', 'audit', 'settings'];

export const PLACEHOLDER_SECTIONS: readonly string[] = NAV.map((n) => n.section).filter((s) => s && !BUILT_SECTIONS.includes(s));

export function navItem(section: string): NavItem | undefined {
  return NAV.find((n) => n.section === section);
}
