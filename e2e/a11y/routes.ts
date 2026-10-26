import { SL_ITEMS, STORES } from '../fixtures/seed';

// Every route of the four apps that a person can open, for axe (e2e/a11y) and the visual baselines
// (e2e/visual). Dynamic routes use seed ids. `auth` is the signed-in person the app needs (null: open
// to a visitor). `visual` marks the screens that also get a baseline (stable content only).
export type AppName = 'customer' | 'business' | 'admin' | 'site';
export type Who = 'asha' | 'owner' | 'superAdmin' | null;

export interface RouteDef {
  app: AppName;
  name: string;
  path: string;
  auth: Who;
  visual?: boolean;
}

const c = (name: string, path: string, auth: Who = 'asha', visual = false): RouteDef => ({ app: 'customer', name, path, auth, visual });
const b = (name: string, path: string, auth: Who = 'owner', visual = false): RouteDef => ({ app: 'business', name, path, auth, visual });
const a = (name: string, path: string, auth: Who = 'superAdmin', visual = false): RouteDef => ({ app: 'admin', name, path, auth, visual });
const s = (name: string, path: string, visual = false): RouteDef => ({ app: 'site', name, path, auth: null, visual });

export const ROUTES: RouteDef[] = [
  c('home', '/', 'asha', true),
  c('search', '/search?q=atta', 'asha', true),
  c('map', '/map'),
  c('shop', `/s/${STORES.SL.slug}`, 'asha', true),
  c('product', `/s/${STORES.SL.slug}/i/${SL_ITEMS.butter.id}`, 'asha', true),
  c('cart', '/cart', 'asha', true),
  c('checkout', '/checkout'),
  c('account', '/account', 'asha', true),
  c('orders', '/orders', 'asha', true),
  c('baskets', '/baskets'),
  c('baskets-device', '/baskets/device'),
  c('asks', '/asks'),
  c('help', '/help'),
  c('help-new', '/help/new'),
  c('language', '/language', null, true),
  c('location', '/location', null),
  c('sign-in', '/sign-in', null, true),

  b('overview', '/overview', 'owner', true),
  b('orders', '/orders', 'owner', true),
  b('sell', '/sell', 'owner', true),
  b('stock', '/stock', 'owner', true),
  b('offers', '/offers'),
  b('customers', '/customers'),
  b('credits', '/credits'),
  b('analytics', '/analytics'),
  b('reviews', '/reviews'),
  b('coach', '/coach'),
  b('voice', '/voice'),
  b('restock-scan', '/restock-scan'),
  b('settings', '/settings', 'owner', true),
  b('standee', '/standee'),
  b('stores', '/stores'),
  b('choose-shop', '/choose-shop'),
  b('onboarding', '/onboarding'),
  b('help', '/help'),
  b('help-new', '/help/new'),
  b('sign-in', '/sign-in', null, true),

  a('dashboard', '/', 'superAdmin', true),
  a('businesses', '/businesses', 'superAdmin', true),
  a('stores', '/stores', 'superAdmin', true),
  a('orders', '/orders'),
  a('customers', '/customers'),
  a('catalogue', '/catalogue'),
  a('credits', '/credits'),
  a('demand', '/demand'),
  a('field', '/field'),
  a('field-new', '/field/new'),
  a('field-agents', '/field-agents'),
  a('jobs', '/jobs'),
  a('notifications', '/notifications'),
  a('payouts', '/payouts'),
  a('referrals', '/referrals'),
  a('reviews', '/reviews'),
  a('risk', '/risk'),
  a('settings', '/settings'),
  a('system', '/system'),
  a('support', '/support'),
  a('audit', '/audit'),
  a('banners', '/banners'),
  a('admin-users', '/admin-users'),
  a('sign-in', '/sign-in', null, true),

  s('home', '/', true),
  s('home-hi', '/hi/'),
  s('home-kn', '/kn/'),
  s('home-te', '/te/'),
  s('home-ta', '/ta/'),
  s('for-retailers', '/for-retailers/', true),
  s('how-it-works', '/how-it-works/'),
  s('pricing', '/pricing/', true),
  s('about', '/about/'),
  s('contact', '/contact/', true),
  s('privacy', '/legal/privacy/'),
  s('terms-shoppers', '/legal/terms-for-shoppers/'),
  s('terms-shops', '/legal/terms-for-shops/'),
  s('refunds', '/legal/refunds-and-credits/'),
];

export const VIEWPORTS_A11Y = {
  'phone-360': { width: 360, height: 740 },
  'phone-390': { width: 390, height: 844 },
  'desktop-1280': { width: 1280, height: 800 },
} as const;
