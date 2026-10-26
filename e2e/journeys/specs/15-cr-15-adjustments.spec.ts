import { Customer, expect, expectWithStepUp, openAdmin, test, waitForCredits } from '../../fixtures/journey';

// CR-15, the E2E part (A-09 adjustment and approval screens; D-05, D-73): a finance admin adds 500
// credits (at the ₹500 threshold, exclusive: it posts at once), then 600 on the Credits screen (above it:
// a pending request, nothing posted). The requester cannot approve their own request; a different
// finance admin approves it on the Pending approvals tab and the wallet gains 600.
interface Ledger {
  items: { id: string; kind: string }[];
}
interface Adjustment {
  outcome: string;
  transactionId: string | null;
}

test('CR-15 +500 credits posts at once; +600 waits for a second finance admin, who approves it on A-09', async ({ who, browser }) => {
  const customer = new Customer(await who('walkIn'));
  const fin1 = await who('finance1');
  const fin2 = await who('finance2');
  const start = (await customer.wallet()).availableCredits;

  const accountId = await test.step('finance finds the customer’s wallet account', async () => {
    const found = await fin1.api.ok<Ledger>('finance finds the wallet', 'GET', '/v1/admin/ledger/accounts', undefined, { params: { q: customer.who.userId } });
    const wallet = found.items.find((a) => a.kind === 'customer_wallet');
    expect(wallet, 'the customer has a wallet account').toBeTruthy();
    return wallet?.id ?? '';
  });

  await test.step('+500 credits is at the threshold: it posts at once', async () => {
    await fin1.stepUp();
    const r = await fin1.api.ok<Adjustment>('finance adds 500 credits', 'POST', '/v1/admin/credit-adjustments', { accountId, amountPaise: 50000, reason: 'CR-15 gate: threshold' });
    expect(r.outcome).toBe('posted');
    await waitForCredits(customer, start + 500);
  });

  const reason = 'CR-15 gate: above the threshold';
  const first = await openAdmin(browser, fin1);
  try {
    await test.step('+600 on the Credits screen is only requested: the wallet does not change', async () => {
      const { page } = first;
      await page.goto(`/credits?account=${accountId}`);
      await page.getByRole('button', { name: 'Adjust credits' }).click();
      const dialog = page.getByRole('dialog', { name: /Adjust credits for/ });
      await dialog.getByLabel('Credits').fill('600');
      await dialog.getByLabel('Reason').fill(reason);
      await dialog.getByRole('button', { name: 'Adjust' }).click();
      await expectWithStepUp(page, fin1, page.getByText(/is waiting for a second person/));
      expect((await customer.wallet()).availableCredits, 'nothing posted yet').toBe(start + 500);
    });

    await test.step('the requester sees the request on Pending approvals and cannot approve it', async () => {
      const { page } = first;
      await page.goto('/credits?tab=approvals');
      const row = page.getByRole('row').filter({ hasText: reason });
      await expect(row).toBeVisible();
      await expect(row.getByRole('button', { name: 'Approve' })).toBeDisabled();
      await expect(row.getByText('Needs a second person')).toBeVisible();
    });
  } finally {
    await first.close();
  }

  const second = await openAdmin(browser, fin2);
  try {
    await test.step('a different finance admin approves it with a note; the wallet gains 600', async () => {
      const { page } = second;
      await page.goto('/credits?tab=approvals');
      const row = page.getByRole('row').filter({ hasText: reason });
      await expect(row.getByRole('button', { name: 'Approve' })).toBeEnabled();
      await row.getByRole('button', { name: 'Approve' }).click();
      const dialog = page.getByRole('dialog', { name: /^Approve .* for .*\?$/ });
      await dialog.locator('textarea').fill('CR-15 gate: checked');
      await dialog.getByRole('button', { name: 'Approve' }).click();
      await expectWithStepUp(page, fin2, page.getByText(/approved and posted/));
      await waitForCredits(customer, start + 1100);
    });
  } finally {
    await second.close();
  }

  await test.step('the request reads approved, with the approver’s note', async () => {
    const list = await fin2.api.ok<{ items: { reason: string; status: string; decisionNote: string | null }[] }>('finance lists the requests', 'GET', '/v1/admin/credit-adjustments', undefined, { params: { status: 'approved' } });
    const mine = list.items.find((i) => i.reason === reason);
    expect(mine?.status).toBe('approved');
    expect(mine?.decisionNote).toBe('CR-15 gate: checked');
  });
});
