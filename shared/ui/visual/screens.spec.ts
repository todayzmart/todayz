import { expect, test } from '@playwright/test';
import { guardPage, shot } from '@todayz/config/playwright';
import { designCards } from './catalog';
import { openStory, settle } from './helpers';

// QA evidence for the report, not a check: runs only when TZ_QA_DIR says where the images go,
// e.g. TZ_QA_DIR=docs/qa/W1-02/screens. Synthetic data only (the names the design previews use).
test.skip(!process.env['TZ_QA_DIR'], 'set TZ_QA_DIR to refresh the QA screenshots');
test.use({ deviceScaleFactor: 2 });

const SHOWN = [
  'ProductCard',
  'StoreCard',
  'CartBar',
  'PickupCode',
  'OrderTimeline',
  'KpiTile',
  'DemandInsights',
  'PosCart',
  'Sidebar',
  'BottomNav',
  'StoreMap',
];
const FILE: Record<string, string> = { KpiTile: 'kpistrip' };

test('Storybook gallery', async ({ page }) => {
  const guard = await guardPage(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/sb/index.html?path=/story/commerce-productcard--design-reference&globals=a11y.manual:!true');
  // Every component group open in the sidebar, so the picture shows the whole kit.
  await page.locator('#storybook-explorer-tree').waitFor();
  for (const id of [
    'inputs-and-actions',
    'status',
    'commerce',
    'map',
    'dashboard',
    'navigation',
    'feedback-and-overlays',
  ]) {
    const group = page.locator(`[data-item-id="${id}"][aria-expanded="false"]`);
    if (await group.count()) await group.click();
  }
  await page.frameLocator('#storybook-preview-iframe').locator('#storybook-root > *').first().waitFor();
  await settle(page);
  await shot(page, 'storybook-gallery.png');
  expect(guard.external).toEqual([]);
});

for (const card of designCards.filter((c) => SHOWN.includes(c.card))) {
  test(`${card.card} design reference`, async ({ page }) => {
    const guard = await guardPage(page);
    await page.setViewportSize({ width: card.width, height: card.height });
    await openStory(page, card.storyId);
    await shot(page, `${FILE[card.card] ?? card.card.toLowerCase()}.png`);
    expect(guard.external).toEqual([]);
    expect(guard.errors).toEqual([]);
  });
}

test('StoreMap full map (MapLibre, offline fixture basemap)', async ({ page }) => {
  const guard = await guardPage(page);
  await page.setViewportSize({ width: 768, height: 500 });
  await openStory(page, 'map-storemap--full-map');
  // WebGL when the browser has it; the static fallback otherwise. Either way it renders offline.
  await page.locator('.tz-map:is([data-mode="webgl"], [data-mode="static"])').waitFor({ timeout: 15_000 });
  await page.waitForTimeout(500);
  await shot(page, 'storemap-full.png');
  expect(guard.external).toEqual([]);
  expect(guard.errors).toEqual([]);
});
