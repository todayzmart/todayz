import { House, ReceiptText, Search, UserRound, Wallet, type LucideIcon } from 'lucide-react';

export interface NavItem {
  /** URL segment; '' is the home route. */
  section: string;
  /** i18n key in shared/i18n/messages/customer/shell.en.json */
  label: string;
  icon: LucideIcon;
}

/** Bottom nav, in the order of 06-screens.md "Customer app global". Five items at most. */
export const NAV: readonly NavItem[] = [
  { section: '', label: 'shell.nav.home', icon: House },
  { section: 'search', label: 'shell.nav.search', icon: Search },
  { section: 'orders', label: 'shell.nav.orders', icon: ReceiptText },
  { section: 'wallet', label: 'shell.nav.wallet', icon: Wallet },
  { section: 'account', label: 'shell.nav.account', icon: UserRound },
];

/**
 * Sections `$section` renders a placeholder for. A WP builds its section by adding a static route
 * file (`src/routes/<area>/…`), which outranks `$section`; nothing here changes (src/app/routing.test.ts).
 */
export const PLACEHOLDER_SECTIONS: readonly string[] = NAV.map((n) => n.section).filter(Boolean);

export function navItem(section: string): NavItem | undefined {
  return NAV.find((n) => n.section === section);
}
