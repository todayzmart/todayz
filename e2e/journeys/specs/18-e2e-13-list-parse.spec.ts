import { Customer, expect, test, waitForDraft, withFlags } from '../../fixtures/journey';

// E2E-13, the AI part (AI-C1, D-110): a typed shopping list is parsed into basket lines by the mock
// model, the customer confirms line by line (accept one, edit one, discard one) and gets the lines for
// the basket editor. Nothing is saved to a basket and nothing is searched until the customer does it.
interface Draft {
  status: string;
  failure: unknown;
  lines: { lineId: string; text: string; quantity: number }[];
}
interface Confirmed {
  status: string;
  lines: { lineId: string; outcome: string }[];
  basketLines: { lineId: string; text: string; quantity: number }[];
}
interface Baskets {
  items?: unknown[];
  baskets?: unknown[];
}

test('E2E-13 AI: a typed list becomes basket lines, confirmed line by line', async ({ who }) => {
  const c = new Customer(await who('baskets'));
  const admin = await who('superAdmin');

  await withFlags(admin, { ai_listparse_enabled: true }, async () => {
    const before = await c.api.ok<Baskets>('customer lists baskets', 'GET', '/v1/me/baskets');
    const count = (before.items ?? before.baskets ?? []).length;

    const draft = await test.step('the customer types a list and the model splits it into lines', async () => {
      const r = await c.api.ok<{ draftId: string }>('customer sends the list', 'POST', '/v1/me/ai/list-parse/drafts', { text: '1 kg atta, 2 Nandini milk, Dolo 650' }, { want: [200, 202] });
      const d = await waitForDraft<Draft>(c.who, `/v1/me/ai/list-parse/drafts/${r.draftId}`);
      expect(d.lines.map((l) => l.quantity)).toEqual([1, 2, 1]);
      return { id: r.draftId, lines: d.lines };
    });

    await test.step('line by line: accept the first, edit the second, discard the third', async () => {
      const [a, b, d] = draft.lines;
      const done = await c.api.ok<Confirmed>('customer confirms the lines', 'POST', `/v1/me/ai/list-parse/drafts/${draft.id}/confirm`, {
        lines: [
          { lineId: a?.lineId, outcome: 'accepted' },
          { lineId: b?.lineId, outcome: 'edited', value: { text: 'Nandini toned milk 500 ml', quantity: 2 } },
          { lineId: d?.lineId, outcome: 'discarded' },
        ],
      });
      expect(done.lines.map((l) => l.outcome)).toEqual(['accepted', 'edited', 'discarded']);
      expect(done.basketLines.map((l) => l.text)).toEqual([a?.text, 'Nandini toned milk 500 ml']);
    });

    await test.step('nothing was saved to a basket by the parse', async () => {
      const after = await c.api.ok<Baskets>('customer lists baskets', 'GET', '/v1/me/baskets');
      expect((after.items ?? after.baskets ?? []).length).toBe(count);
    });
  });
});
