import type { Locator, Page } from '@playwright/test';

/**
 * Story canvas URL in the built Storybook. `a11y.manual` stops the a11y addon from running its
 * own axe pass after each render, which would race the suite's pass ("Axe is already running").
 */
export const storyUrl = (id: string) =>
  `/sb/iframe.html?id=${encodeURIComponent(id)}&viewMode=story&globals=a11y.manual:!true`;

/** Waits for the story to render, its fonts to load and two frames to settle. */
export async function openStory(page: Page, id: string) {
  await page.goto(storyUrl(id));
  await page.locator('#storybook-root > *').first().waitFor({ state: 'attached' });
  await settle(page);
}

export async function settle(page: Page) {
  await page.evaluate(async () => {
    await document.fonts.ready;
    await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
  });
}

/**
 * Areas the comparison ignores: icons (the design draws Lucide-style stand-ins by hand, the kit
 * uses Lucide itself) and, for the map card, the map drawing (the design paints a sketch; the kit
 * draws real geometry). Both are masked at the same boxes on the design page and the story.
 */
export function masks(page: Page, card: string): Locator[] {
  const list = [page.locator('svg.tz-ico')];
  if (card === 'StoreMap') list.push(page.locator('.tz-map'));
  return list;
}
