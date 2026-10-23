// Production builds only (package.json "build"): removes the Open Graph images of machine-draft
// locales that public/og/ carries for build:e2e and dev. Astro already builds no pages, sitemap
// entries or links for them (siteLocales); verify-dist then fails on any trace that is left.
import { readdirSync, rmSync } from 'node:fs';
import { join } from 'node:path';

const { LOCALES, isMachineDraft } = await import('../src/i18n/index.ts');
const drafts = LOCALES.filter((l) => isMachineDraft(l));
const og = join(import.meta.dirname, '..', 'dist', 'og');
const dropped = readdirSync(og).filter((f) => drafts.some((l) => f.endsWith(`-${l}.png`)));
for (const f of dropped) rmSync(join(og, f));
console.log(`site: dropped ${dropped.length} OG image(s) of draft locales (${drafts.join(', ') || 'none'})`);
