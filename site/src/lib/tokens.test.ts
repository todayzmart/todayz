import { undefinedVars } from '@todayz/tokens/css-vars';
import { readFileSync, readdirSync } from 'node:fs';
import { createRequire } from 'node:module';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect, it } from 'vitest';

const require = createRequire(import.meta.url);
const read = (p: string) => readFileSync(p, 'utf8');
const src = fileURLToPath(new URL('..', import.meta.url));

it('every CSS variable in the site styles is declared by @todayz/tokens', () => {
  const files = readdirSync(src, { recursive: true, encoding: 'utf8' }).filter((f) => /\.(astro|css)$/.test(f));
  const styles = files
    .map((f) => read(join(src, f)))
    .flatMap((text) => [...text.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)].map((m) => m[1] ?? ''))
    .join('\n');
  expect(styles.length).toBeGreaterThan(0);
  const tokens = read(require.resolve('@todayz/tokens/tokens.css'));
  const bundle = read(require.resolve('@todayz/tokens/bundle.css'));
  expect(undefinedVars(styles, tokens, bundle)).toEqual([]);
});
