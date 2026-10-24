import { customerFirstRun, expect, openApp, test, URLS } from '../../fixtures/journey';

// E2E-16 (builder-written): the customer loses the network. The shell shows the offline banner,
// what was loaded stays on screen, and the banner goes when the network is back.
test('E2E-16 offline customer sees the offline banner and keeps the page; it clears when back online', async ({ who, browser }) => {
  const app = await openApp(browser, await who('asha'), URLS.customer);
  const { page, context } = app;
  try {
    await customerFirstRun(page);
    await page.goto('/orders');
    const heading = page.getByRole('heading', { level: 1 }).first();
    await expect(heading).toBeVisible();

    await context.setOffline(true);
    await expect(page.getByText("You're offline.")).toBeVisible();
    await expect(heading).toBeVisible();

    await context.setOffline(false);
    await expect(page.getByText("You're offline.")).toHaveCount(0);
  } finally {
    await context.setOffline(false);
    await app.close();
  }
});
