import { Customer, expect, serverNow, Shop, test, waitForDraft, withFlags } from '../../fixtures/journey';
import { QA_POINT, SL_ITEMS, STORES } from '../../fixtures/seed';

// E2E-14 (AI-R2, D-99): the owner photographs a supplier bill → the mock model reads one line (Amul
// Butter) → the owner confirms it with an expiry two days out → stock and a batch are added → the
// Insights "going to waste" row lists that batch → a flash offer on the batch → the customer's search
// shows the item with an offer. The mocks' supplier_bill fixture (added by W3-14) answers the model call;
// the photo is a 1-pixel PNG because the mock never looks at it.
interface Draft {
  status: string;
  failure: unknown;
  lines: { lineId: string; match: { storeItemId: string } | null }[];
}
interface Batches {
  items: { id: string; itemId: string; quantity: number; expiresOn: string | null }[];
}
interface Insights {
  items: { kind: string; action: { type: string; payload: { batchIds?: string[] } } | null }[];
}
interface Search {
  results: { store: { storeId: string }; items: { storeItemId: string; hasOffer: boolean }[] }[];
}

const PNG = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64');

/** The IST calendar date `days` from the server's now. */
async function istDate(days: number): Promise<string> {
  const ms = (await serverNow()) + (5.5 * 60 + days * 24 * 60) * 60_000;
  return new Date(ms).toISOString().slice(0, 10);
}

test('E2E-14 supplier bill scan → stock and batch → going to waste → flash offer → customer sees the offer', async ({ who }) => {
  const owner = await who('owner');
  const admin = await who('superAdmin');
  const customer = new Customer(await who('asha'));
  const sl = new Shop(owner, STORES.SL);
  const item = SL_ITEMS.butter;
  const expiresOn = await istDate(2);
  let offerId = '';

  try {
    await withFlags(admin, { ai_billscan_enabled: true }, async () => {
      const draftId = await test.step('the owner sends a photo of the bill', async () => {
        const r = await owner.api.call<{ draftId: string }>('POST', `${sl.base}/ai/supplier-bill/drafts`, undefined, {
          idem: true,
          multipart: { photo: { name: 'bill.png', mimeType: 'image/png', buffer: PNG } },
        });
        expect(r.status, `POST supplier-bill drafts answered ${r.status} ${r.code}`).toBe(202);
        return r.body.draftId;
      });

      const draftPath = `${sl.base}/ai/supplier-bill/drafts/${draftId}`;
      await test.step('the draft matches the line to Amul Butter; the owner confirms it with an expiry', async () => {
        const d = await waitForDraft<Draft>(owner, draftPath);
        const line = d.lines[0];
        expect(line?.match?.storeItemId, 'the line matched the store item').toBe(item.id);
        const done = await owner.api.ok<{ lines: { outcome: string }[] }>('owner confirms the bill', 'POST', `${draftPath}/confirm`, {
          lines: [{ lineId: line?.lineId, outcome: 'edited', value: { storeItemId: item.id, quantity: 12, costPaise: 5200, expiresOn } }],
        });
        expect(done.lines[0]?.outcome).toBe('edited');
      });
    });

    const batchId = await test.step('stock and a batch with that expiry were added', async () => {
      const list = await owner.api.ok<Batches>('owner lists the batches', 'GET', `${sl.base}/batches`, undefined, { params: { itemId: item.id } });
      const batch = list.items.find((b) => b.expiresOn === expiresOn);
      expect(batch, 'a batch expiring in two days').toBeTruthy();
      expect(batch?.quantity).toBeGreaterThanOrEqual(12);
      return batch?.id ?? '';
    });

    await test.step('Insights shows going to waste with that batch, and offers a flash offer', async () => {
      const list = await owner.api.ok<Insights>('owner reads the insights', 'GET', `${sl.base}/insights`, undefined, { params: { all: 1, kind: 'going_to_waste' } });
      const row = list.items.find((i) => i.kind === 'going_to_waste');
      expect(row, 'a going-to-waste row').toBeTruthy();
      expect(row?.action?.type).toBe('flash_offer');
      expect(row?.action?.payload.batchIds).toContain(batchId);
    });

    await test.step('the owner makes a 20% flash offer on the batch', async () => {
      const o = await owner.api.ok<{ id: string; status: string; source: string }>('owner makes the flash offer', 'POST', `${sl.base}/offers`, {
        kind: 'percent',
        value: 20,
        scope: 'items',
        storeItemIds: [item.id],
        batchId,
      });
      expect(o.status).toBe('live');
      offerId = o.id;
    });

    await test.step('the customer finds the item with an offer', async () => {
      await expect
        .poll(
          async () => {
            const found = await customer.api.ok<Search>('customer searches', 'GET', '/v1/discover/search', undefined, { params: { q: 'amul butter', lat: QA_POINT.lat, lng: QA_POINT.lng } });
            return found.results.filter((r) => r.store.storeId === STORES.SL.id).flatMap((r) => r.items).find((i) => i.storeItemId === item.id)?.hasOffer ?? false;
          },
          { message: 'the butter shows an offer', timeout: 20_000 },
        )
        .toBe(true);
    });
  } finally {
    if (offerId) await owner.api.call('PATCH', `${sl.base}/offers/${offerId}`, { status: 'ended' }, { ifMatch: '*' });
  }
});
