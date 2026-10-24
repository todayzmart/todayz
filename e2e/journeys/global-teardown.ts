import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';

import { moveClock, Shop } from '../fixtures/journey';
import { disposeAll, principal, removeState } from '../fixtures/principals';
import { PEOPLE, STORES } from '../fixtures/seed';

// Puts the test clock back, restores the shops' seeded hours and deletes the session file.
export default async function globalTeardown() {
  try {
    await moveClock({ reset: true }, 0);
    const file = join(tmpdir(), `todayz-journeys-${process.env['TZ_JOURNEYS_RUN']}`, 'hours.json');
    if (existsSync(file)) {
      const before = JSON.parse(readFileSync(file, 'utf8')) as Record<string, unknown>;
      const owner = await principal(PEOPLE.owner);
      for (const store of [STORES.SL, STORES.SL2]) {
        if (before[store.id]) await new Shop(owner, store).patchSettings({ hours: before[store.id] });
      }
    }
  } finally {
    await disposeAll();
    removeState();
  }
}
