// @vitest-environment node
import { undefinedVars } from '@todayz/tokens/css-vars';
import { readFileSync, readdirSync } from 'node:fs';
import { createRequire } from 'node:module';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect, it } from 'vitest';

const require = createRequire(import.meta.url);
const read = (p: string) => readFileSync(p, 'utf8');
const src = fileURLToPath(new URL('..', import.meta.url));

it('every CSS variable the app uses is declared by @todayz/tokens', () => {
  const files = readdirSync(src, { recursive: true, encoding: 'utf8' }).filter((f) => f.endsWith('.css'));
  expect(files.length).toBeGreaterThan(0);
  const app = files.map((f) => read(join(src, f))).join('\n');
  const tokens = read(require.resolve('@todayz/tokens/tokens.css'));
  const bundle = read(require.resolve('@todayz/tokens/bundle.css'));
  expect(undefinedVars(app, tokens, bundle)).toEqual([]);
});
