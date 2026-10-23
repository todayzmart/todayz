// Every story renders, and axe finds no violations in it (SC-UI-02). Colour contrast needs real
// layout, so it runs in the Playwright visual suite against the built Storybook instead. The
// region rule (all content inside landmarks) is a page rule: a component alone has no page.
import { composeStories, type Meta } from '@storybook/react-vite';
import { act, render } from '@testing-library/react';
import axe from 'axe-core';
import type { ComponentType } from 'react';
import { describe, expect, it } from 'vitest';

type StoryModule = { default: Meta } & Record<string, unknown>;

const modules = import.meta.glob<StoryModule>('../**/*.stories.tsx', { eager: true });

async function violations(container: HTMLElement) {
  const result = await axe.run(container, {
    rules: { 'color-contrast': { enabled: false }, region: { enabled: false } },
    resultTypes: ['violations'],
  });
  return result.violations.map((v) => `${v.id}: ${v.help} (${v.nodes.map((n) => n.target.join(' ')).join(', ')})`);
}

describe.each(Object.entries(modules))('%s', (_path, mod) => {
  const stories = Object.entries(composeStories(mod)) as [string, ComponentType & { tags?: string[] }][];

  it('exports stories', () => {
    expect(stories.length).toBeGreaterThan(0);
  });

  it.each(stories)('%s renders without axe violations', async (_name, Story) => {
    const { container } = render(<Story />);
    // Let effects (ResizeObserver fallbacks, dialogs opening) settle.
    await act(async () => {
      await Promise.resolve();
    });
    expect(container.firstChild).not.toBeNull();
    expect(await violations(document.body)).toEqual([]);
  });
});
