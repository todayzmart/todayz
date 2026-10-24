import { writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';

import { Shop } from '../fixtures/journey';
import { disposeAll, principal } from '../fixtures/principals';
import { ALL_DAY, PEOPLE, STORES } from '../fixtures/seed';

// Opens Sri Lakshmi and Sri Lakshmi 2 all day (the gate runs at any hour, and the clock journeys
// move the test clock by up to 12 hours), keeping their seeded hours to put back in teardown.
export default async function globalSetup() {
  const owner = await principal(PEOPLE.owner);
  const before: Record<string, unknown> = {};
  for (const store of [STORES.SL, STORES.SL2]) {
    const shop = new Shop(owner, store);
    before[store.id] = (await shop.settings()).body['hours'];
    await shop.patchSettings({ hours: ALL_DAY });
  }
  writeFileSync(join(tmpdir(), `todayz-journeys-${process.env['TZ_JOURNEYS_RUN']}`, 'hours.json'), JSON.stringify(before), { mode: 0o600 });
  await disposeAll();
}
