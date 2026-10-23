import { writeFileSync } from 'node:fs';
import { expect, test, type Page } from '@playwright/test';
import { guardPage } from '@todayz/config/playwright';
import pixelmatch from 'pixelmatch';
import { PNG } from 'pngjs';
import { designCards, type DesignCard } from './catalog';
import { masks, openStory, settle } from './helpers';

/** 08-testing: a story may differ from its design card in at most 0.1% of pixels. */
const MAX_DIFF = 0.001;

async function capture(page: Page, card: DesignCard) {
  const png = await page.screenshot({
    animations: 'disabled',
    caret: 'hide',
    mask: masks(page, card.card),
    maskColor: 'magenta',
  });
  return PNG.sync.read(png);
}

for (const card of designCards) {
  test(`${card.card} story matches its design card`, async ({ page }, info) => {
    const guard = await guardPage(page);
    await page.setViewportSize({ width: card.width, height: card.height });

    await page.goto(`/design/${card.card}`);
    await settle(page);
    const design = await capture(page, card);

    await openStory(page, card.storyId);
    const story = await capture(page, card);

    const { width, height } = design;
    const diff = new PNG({ width, height });
    const changed = pixelmatch(design.data, story.data, diff.data, width, height, { threshold: 0.1 });
    const ratio = changed / (width * height);
    if (ratio > MAX_DIFF) {
      // Saved next to the failure (visual/out/<test>/) so they can be opened side by side.
      for (const [name, png] of [
        ['design', design],
        ['story', story],
        ['diff', diff],
      ] as const) {
        writeFileSync(info.outputPath(`${name}.png`), PNG.sync.write(png));
      }
    }
    expect(ratio, `${changed} of ${width * height} pixels differ`).toBeLessThanOrEqual(MAX_DIFF);
    expect(guard.external).toEqual([]);
    expect(guard.errors).toEqual([]);
  });
}
