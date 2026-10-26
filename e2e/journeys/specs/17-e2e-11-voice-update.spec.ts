import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { Customer, expect, Shop, test, waitForDraft, withFlags } from '../../fixtures/journey';
import { seedItemId, STORES } from '../../fixtures/seed';

// E2E-11 (AI-R1, D-65): the owner speaks a stock update (mock speech and model) → a draft of lines →
// edits the line that named no item to a stale item → confirms → the stock changes and the shop's
// freshness stamp is new: the customer who saw "stock not confirmed" for that item now sees it confirmed.
// The audio is the mocks' Hinglish sample; the model's lines are the mocks' fixture (one unmatched sale,
// one with a placeholder item). The draft is created through the API (the recorder needs a microphone).
interface Draft {
  status: string;
  failure: unknown;
  lines: { lineId: string; kind: string; storeItemId: string | null; matched: boolean; disabled: string | null }[];
}
interface Items {
  items: { storeItemId: string; confirmedAt: string | null }[];
}

const STALE = seedItemId(STORES.SL2, 12);

test('E2E-11 voice update: draft → edit → confirm → stock changes → customer sees the item confirmed', async ({ who }) => {
  const owner = await who('owner');
  const admin = await who('superAdmin');
  const customer = new Customer(await who('asha'));
  const sl2 = new Shop(owner, STORES.SL2);
  const itemsPath = `/v1/discover/stores/${STORES.SL2.slug}/items`;
  // The whole catalogue in one page (Sri Lakshmi 2 lists 22 items).
  const params = { limit: 50 };

  await test.step('the customer sees the item as not confirmed recently', async () => {
    const list = await customer.api.ok<Items>('customer reads the shop items', 'GET', itemsPath, undefined, { params });
    const row = list.items.find((i) => i.storeItemId === STALE);
    expect(row, 'the stale seed item is listed').toBeTruthy();
    expect(row?.confirmedAt, 'stock is stale').toBeNull();
  });

  await withFlags(admin, { ai_voice_stock_enabled: true, feature_voice_enabled: true }, async () => {
    const draftId = await test.step('the owner records an update: the recording is sent', async () => {
      await owner.api.ok('voice update is on', 'GET', `${sl2.base}/ai/voice-stock`);
      const audio = readFileSync(resolve(process.cwd(), '../mocks/fixtures/stt/stock-update-hinglish.wav'));
      const r = await owner.api.call<{ draftId: string }>('POST', `${sl2.base}/ai/voice-stock/drafts`, undefined, {
        idem: true,
        multipart: { audio: { name: 'update.wav', mimeType: 'audio/wav', buffer: audio }, language: 'hi-IN' },
      });
      expect(r.status, `POST drafts answered ${r.status} ${r.code}`).toBe(202);
      return r.body.draftId;
    });

    const draftPath = `${sl2.base}/ai/voice-stock/drafts/${draftId}`;
    const draft = await test.step('the draft is ready with lines to review', async () => {
      const d = await waitForDraft<Draft>(owner, draftPath);
      expect(d.lines.length).toBeGreaterThan(0);
      return d;
    });

    await test.step('the owner picks the item for the first line, drops the rest, and confirms', async () => {
      const [first, ...rest] = draft.lines;
      expect(first, 'a first line').toBeTruthy();
      const lines = [
        { lineId: first?.lineId, outcome: 'edited', value: { kind: 'sale', storeItemId: STALE, quantity: 1 } },
        ...rest.map((l) => ({ lineId: l.lineId, outcome: 'discarded' })),
      ];
      const done = await owner.api.ok<{ status: string; lines: { outcome: string }[] }>('owner confirms the draft', 'POST', `${draftPath}/confirm`, { lines });
      expect(done.lines[0]?.outcome).toBe('edited');
    });
  });

  await test.step('the customer now sees the item confirmed', async () => {
    await expect
      .poll(
        async () => {
          const list = await customer.api.ok<Items>('customer reads the shop items', 'GET', itemsPath, undefined, { params });
          return list.items.find((i) => i.storeItemId === STALE)?.confirmedAt ?? null;
        },
        { message: 'confirmedAt is set', timeout: 20_000 },
      )
      .not.toBeNull();
  });
});
