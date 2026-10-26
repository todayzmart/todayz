import { writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';

import { Shop } from '../fixtures/journey';
import { disposeAll, principal } from '../fixtures/principals';
import { ALL_DAY, ANNAPURNA, LAKSHMI, PEOPLE, STORES } from '../fixtures/seed';

// The shops the journeys order from, each with the owner who can change its hours.
export const OPEN_ALL_DAY = [
  { store: STORES.SL, owner: PEOPLE.owner, businessId: LAKSHMI.id },
  { store: STORES.SL2, owner: PEOPLE.owner, businessId: LAKSHMI.id },
  { store: STORES.AP, owner: PEOPLE.apOwner, businessId: ANNAPURNA.id },
];

// Opens Sri Lakshmi, Sri Lakshmi 2 and Annapurna all day (the gate runs at any hour, and the clock
// journeys move the test clock by up to 12 hours), keeping their seeded hours to put back in teardown.
export default async function globalSetup() {
  const before: Record<string, unknown> = {};
  for (const { store, owner, businessId } of OPEN_ALL_DAY) {
    const shop = new Shop(await principal(owner), store, businessId);
    before[store.id] = (await shop.settings()).body['hours'];
    await shop.patchSettings({ hours: ALL_DAY });
  }
  writeFileSync(join(tmpdir(), `todayz-journeys-${process.env['TZ_JOURNEYS_RUN']}`, 'hours.json'), JSON.stringify(before), { mode: 0o600 });
  await disposeAll();
}
