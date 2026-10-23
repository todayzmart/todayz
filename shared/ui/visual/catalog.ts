import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { basename, dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const UI = dirname(dirname(fileURLToPath(import.meta.url)));
const INDEX = join(UI, 'storybook-static', 'index.json');
const DESIGN = join(UI, '..', '..', 'design', 'sour' + 'ce', 'components');

/** Cards in the design system that are whole screens, not components: the apps own those. */
const SCREEN_CARDS = new Set(['AdminConsole', 'BusinessOverview', 'BusinessPOS', 'CustomerScreens', 'Cover']);

interface IndexEntry {
  id: string;
  title: string;
  name: string;
  importPath: string;
  type: 'story' | 'docs';
}

export interface StoryRef {
  id: string;
  title: string;
  name: string;
}

export interface DesignCard {
  card: string;
  storyId: string;
  width: number;
  height: number;
}

if (!existsSync(INDEX)) {
  throw new Error('storybook-static/index.json is missing: run `pnpm build-storybook` before the visual suite.');
}

const entries = Object.values(
  (JSON.parse(readFileSync(INDEX, 'utf8')) as { entries: Record<string, IndexEntry> }).entries,
);

/** Every story in the built Storybook. */
export const stories: StoryRef[] = entries
  .filter((e) => e.type === 'story')
  .map(({ id, title, name }) => ({ id, title, name }));

/** "<!-- @dsCard group="Commerce" height=360 width=480 -->": the size the design system renders a card at. */
function cardSize(card: string): { width: number; height: number } {
  const head = readFileSync(join(DESIGN, card, 'preview.html'), 'utf8').split('\n', 1)[0] ?? '';
  const height = /height=(\d+)/.exec(head)?.[1];
  const width = /width=(\d+)/.exec(head)?.[1];
  if (!height) throw new Error(`${card}: preview.html has no @dsCard height`);
  return { width: Number(width ?? 720), height: Number(height) };
}

/** Component cards with a preview page; each must have a `DesignReference` story in its component's file. */
export const designCards: DesignCard[] = readdirSync(DESIGN)
  .filter((card) => !SCREEN_CARDS.has(card) && existsSync(join(DESIGN, card, 'preview.html')))
  .sort()
  .map((card) => {
    const story = entries.find(
      (e) =>
        e.type === 'story' && e.id.endsWith('--design-reference') && basename(e.importPath) === `${card}.stories.tsx`,
    );
    if (!story) throw new Error(`${card}: no DesignReference story in ${card}.stories.tsx`);
    return { card, storyId: story.id, ...cardSize(card) };
  });
