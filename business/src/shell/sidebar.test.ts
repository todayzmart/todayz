// @vitest-environment node
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const read = (f: string) => readFileSync(fileURLToPath(new URL(f, import.meta.url)), 'utf8');

/** The dark green sidebar (D-269): the founder's override of the white one, drawn with the rail tokens. */
describe('the dark green sidebar', () => {
  const css = read('./shell.module.css');
  const block = css.slice(css.indexOf('*/', css.indexOf('The dark green sidebar')) + 2);

  it('paints the sidebar with the rail tokens, scoped so it outranks the bright skin', () => {
    expect(block).toMatch(/\.app\[data-skin\] \.side \{[^}]*background: var\(--rail\);[^}]*color: var\(--on-rail\);/);
    const selectors = [...block.matchAll(/^([^\s@/*}][^{]*)\{/gm)].map((m) => (m[1] ?? '').trim());
    expect(selectors.length).toBeGreaterThan(5);
    for (const s of selectors) for (const part of s.split(/,\n/)) expect(part.trim(), s).toMatch(/^\.app\[data-skin\] \.side\b/);
  });

  it('uses tokens only, and turns the focus ring to the accent the blue ring is too dark on green', () => {
    expect(css).not.toMatch(/#[0-9a-f]{3,8}\b|rgb\(|hsl\(/i);
    expect(block).toMatch(/outline-color: var\(--rail-accent\)/);
  });

  it('the onboarding rail is drawn with the same tokens', () => {
    const onb = read('../onboarding/onboarding.module.css');
    expect(onb).not.toMatch(/#[0-9a-f]{3,8}\b|rgb\(|hsl\(/i);
    expect(onb).toMatch(/\.rail \{[^}]*background: var\(--rail\);/);
  });
});
