import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';

import { moveClock, Shop } from '../fixtures/journey';
import { disposeAll, principal, removeState } from '../fixtures/principals';
import { OPEN_ALL_DAY } from './global-setup';

// Puts the test clock back, restores the shops' seeded hours and deletes the session file.
export default async function globalTeardown() {
  try {
    await moveClock({ reset: true }, 0);
    const file = join(tmpdir(), `todayz-journeys-${process.env['TZ_JOURNEYS_RUN']}`, 'hours.json');
    if (existsSync(file)) {
      const before = JSON.parse(readFileSync(file, 'utf8')) as Record<string, unknown>;
      for (const { store, owner, businessId } of OPEN_ALL_DAY) {
        if (before[store.id]) await new Shop(await principal(owner), store, businessId).patchSettings({ hours: before[store.id] });
      }
    }
  } finally {
    await disposeAll();
    removeState();
  }
}
