import { readdirSync, readFileSync, statSync } from 'node:fs';
import { createRequire } from 'node:module';
import { join } from 'node:path';
import { undefinedVars } from '@todayz/tokens/css-vars';
import { describe, expect, it } from 'vitest';

// Vitest runs from the package root (happy-dom gives import.meta.url a non-file scheme).
const root = process.cwd();
const require = createRequire(join(root, 'package.json'));
const src = join(root, 'src');
const read = (p: string) => readFileSync(p, 'utf8');
const stripComments = (s: string) => s.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');

function files(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    return statSync(p).isDirectory() ? files(p) : [p];
  });
}

describe('styles.css', () => {
  const css = read(join(src, 'styles.css'));

  it('reads only custom properties the tokens declare', () => {
    const tokens = read(require.resolve('@todayz/tokens/tokens.css'));
    const bundle = read(require.resolve('@todayz/tokens/bundle.css'));
    expect(undefinedVars(css, tokens, bundle)).toEqual([]);
  });

  it('has no colour literals and no dark theme', () => {
    const body = stripComments(css);
    expect(body).not.toMatch(/#[0-9a-f]{3,8}\b/i);
    expect(body).not.toMatch(/\b(?:rgb|rgba|hsl|hsla|oklch)\(/i);
    expect(body).not.toMatch(/prefers-color-scheme/);
  });
});

describe('component sources', () => {
  it('hold no raw hex colours (tokens only)', () => {
    const offenders = files(src)
      .filter((p) => /\.(tsx?|css)$/.test(p) && !/\.test\.tsx?$/.test(p))
      .filter((p) => /#[0-9a-f]{3,8}\b/i.test(stripComments(read(p))));
    expect(offenders).toEqual([]);
  });
});
