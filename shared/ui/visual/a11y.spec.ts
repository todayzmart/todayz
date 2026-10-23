import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { guardPage } from '@todayz/config/playwright';
import { stories } from './catalog';
import { openStory } from './helpers';

// Unit tests already run axe on every story in happy-dom, which cannot judge colour contrast.
// Here every story runs in Chromium with the real tokens, so contrast is checked too.
const WCAG = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];

/**
 * Findings that come from the design system's own CSS (@todayz/tokens bundle.css), which the kit
 * must match pixel for pixel and does not own. Each is matched by story, rule and node, so any
 * other finding still fails. Raised with the lead in docs/qa/W1-02/REPORT.md; remove an entry
 * once the design changes.
 */
const DESIGN_EXCEPTIONS: { title: string; rule: string; html: RegExp; why: string }[] = [
  {
    title: 'Commerce/CartBar',
    rule: 'color-contrast',
    html: /^<small>/,
    why: '.tz-cartbar-text small: on-brand at 85% opacity on brand, 4.39:1 for 12px text (AA needs 4.5:1)',
  },
  {
    title: 'Commerce/CartBar',
    rule: 'color-contrast',
    html: /^<span class="tz-cartbar-count"/,
    why: '.tz-cartbar-count: on-brand on a 22% on-brand wash, 3.6:1 for 14px bold (AA needs 4.5:1)',
  },
];

function allowed(title: string, rule: string, html: string) {
  return DESIGN_EXCEPTIONS.some((e) => e.title === title && e.rule === rule && e.html.test(html));
}

for (const story of stories) {
  test(`axe: ${story.title} / ${story.name}`, async ({ page }) => {
    const guard = await guardPage(page);
    await openStory(page, story.id);
    const { violations } = await new AxeBuilder({ page }).withTags(WCAG).analyze();
    const found = violations.flatMap((v) =>
      v.nodes.filter((n) => !allowed(story.title, v.id, n.html)).map((n) => `${v.id}: ${n.target.join(' ')}`),
    );
    expect(found).toEqual([]);
    expect(guard.external).toEqual([]);
    expect(guard.errors).toEqual([]);
  });
}
