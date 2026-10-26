// The dev seed the journeys run on (backend/internal/seed). Ids, slugs and prices are fixed by the
// seed; phones are the reserved test numbers (+91 90000000xx, 01 §2). Never print a phone.

export const URLS = {
  api: process.env['TZ_API_URL'] ?? 'https://api.todayz.localhost',
  mocks: process.env['TZ_MOCKS_URL'] ?? 'http://127.0.0.1:4010',
  customer: process.env['TZ_CUSTOMER_URL'] ?? 'https://app.todayz.localhost',
  business: process.env['TZ_BUSINESS_URL'] ?? 'https://business.todayz.localhost',
  admin: process.env['TZ_ADMIN_URL'] ?? 'https://admin.todayz.localhost',
} as const;

export type App = 'customer' | 'business' | 'admin';

/** A person the journeys sign in as. `national` is the 10-digit number. */
export interface Person {
  key: string;
  national: string;
  app: App;
  /** The key of the admin in e2e/fixtures/admins.yaml whose test TOTP secret signs this person in. */
  admin?: string;
}

export const PEOPLE = {
  asha: { key: 'asha', national: '9000000001', app: 'customer' },
  ravi: { key: 'ravi', national: '9000000002', app: 'customer' },
  owner: { key: 'owner', national: '9000000011', app: 'business' },
  manager: { key: 'manager', national: '9000000012', app: 'business' },
  cashier: { key: 'cashier', national: '9000000013', app: 'business' },
  support: { key: 'support', national: '9000000022', app: 'admin', admin: 'admin.support' },
  superAdmin: { key: 'superAdmin', national: '9000000028', app: 'admin', admin: 'admin.super_admin_1' },
  // Wave 3 gate (W3-14): the other seeded admins (admins.yaml) and the people the new journeys create.
  fieldAgent: { key: 'fieldAgent', national: '9000000021', app: 'admin', admin: 'admin.field_agent' },
  finance1: { key: 'finance1', national: '9000000026', app: 'admin', admin: 'admin.finance_1' },
  finance2: { key: 'finance2', national: '9000000027', app: 'admin', admin: 'admin.finance_2' },
  // A shopkeeper the field agent onboards (E2E-08): a new number, so a new business-app account.
  newOwner: { key: 'newOwner', national: '9000000048', app: 'business' },
  // The shopper who scans the standee (E2E-01).
  standee: { key: 'standee', national: '9000000047', app: 'customer' },
  // New customers (no account in the seed): each journey that changes a customer's standing gets its own.
  referee: { key: 'referee', national: '9000000041', app: 'customer' },
  noShow: { key: 'noShow', national: '9000000042', app: 'customer' },
  walkIn: { key: 'walkIn', national: '9000000043', app: 'customer' },
  partial: { key: 'partial', national: '9000000044', app: 'customer' },
  baskets: { key: 'baskets', national: '9000000045', app: 'customer' },
  delivery: { key: 'delivery', national: '9000000046', app: 'customer' },
  blocked: { key: 'blocked', national: '9000000056', app: 'customer' },
} as const satisfies Record<string, Person>;

export const ASHA_REFERRAL_CODE = 'ASHA22';

export const LAKSHMI = { id: '01KE0S1W6W8ZV7T1C3A6MB0001', name: 'Lakshmi Traders' } as const;

export interface SeedStore {
  id: string;
  slug: string;
  name: string;
  no: number;
}

export const STORES = {
  SL: { id: '01KE0S1W6W8ZV7T1C3A6MS0001', slug: 'sri-lakshmi-indiranagar', name: 'Sri Lakshmi', no: 1 },
  SL2: { id: '01KE0S1W6W8ZV7T1C3A6MS0002', slug: 'sri-lakshmi-2-hal-2nd-stage', name: 'Sri Lakshmi 2', no: 2 },
  SS: { id: '01KE0S1W6W8ZV7T1C3A6MS0010', slug: 'suspended-store-indiranagar', name: 'Suspended Store', no: 0 },
} as const satisfies Record<string, SeedStore>;

/** The QA point in Indiranagar the seed's shops sit around (PIN 560038). */
export const QA_POINT = { lat: 12.9719, lng: 77.6412 } as const;

const CROCKFORD = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

/** A seeded store item's id: storeNo × 100 + position (1-based), as seedStoreItems numbers them. */
export function seedItemId(store: SeedStore, position: number): string {
  let n = store.no * 100 + position;
  let tail = '';
  for (let i = 0; i < 4; i++) {
    tail = (CROCKFORD[n % 32] ?? '0') + tail;
    n = Math.floor(n / 32);
  }
  return `01KE0S1W6W8ZV7T1C3A6MQ${tail}`;
}

/** Sri Lakshmi's items by the manual book's short names (I1…I14). */
export const SL_ITEMS = {
  butter: { id: seedItemId(STORES.SL, 1), name: 'Amul Butter 100 g', pricePaise: 5800 },
  maggi: { id: seedItemId(STORES.SL, 2), name: 'Maggi 2-Minute Noodles 70 g', pricePaise: 1400 },
  milk: { id: seedItemId(STORES.SL, 3), name: 'Nandini Toned Milk 500 ml', pricePaise: 2400 },
  atta: { id: seedItemId(STORES.SL, 4), name: 'Wheat atta 5 kg', pricePaise: 24500 },
  cola: { id: seedItemId(STORES.SL, 5), name: 'Coca-Cola 750 ml', pricePaise: 4000 },
  curd: { id: seedItemId(STORES.SL, 8), name: 'Curd 400 g', pricePaise: 3500 },
  qa100: { id: seedItemId(STORES.SL, 10), name: 'QA pack ₹100.00', pricePaise: 10000 },
  qa120: { id: seedItemId(STORES.SL, 13), name: 'QA pack ₹120', pricePaise: 12000 },
} as const;

/** Sri Lakshmi 2's items (J1…J22). */
export const SL2_ITEMS = {
  tea: { id: seedItemId(STORES.SL2, 1), name: 'Tata Tea Gold 250 g', pricePaise: 14500 },
  milk: { id: seedItemId(STORES.SL2, 20), name: 'Nandini Toned Milk 500 ml', pricePaise: 2400 },
  bread: { id: seedItemId(STORES.SL2, 18), name: 'Bread 400 g', pricePaise: 4500 },
  eggs: { id: seedItemId(STORES.SL2, 19), name: 'Eggs tray 12', pricePaise: 8400 },
  parleG: { id: seedItemId(STORES.SL2, 21), name: 'Parle-G', pricePaise: 1000 },
} as const;

/** Open every day, all day: the journeys run at any hour (docs/qa/stack-2026-10-24). */
export const ALL_DAY = {
  weekly: Object.fromEntries(['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'].map((d) => [d, [{ open: '00:00', close: '00:00' }]])),
  holidays: [],
};
