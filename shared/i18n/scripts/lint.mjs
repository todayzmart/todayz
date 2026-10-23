#!/usr/bin/env node
// Message lint (`pnpm --filter @todayz/i18n lint:messages`, part of the package's lint step, so of
// `make check`): missing keys, broken translations, review status. Exit 1 on any error; warnings
// (untranslated, machine drafts, unused) are counted, `--verbose` lists them.
import { readdir } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { lintApp } from './catalogue.mjs';

const pkg = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const repo = resolve(pkg, '..', '..');
const messagesDir = join(pkg, 'messages');

const verbose = process.argv.includes('--verbose');
let failed = false;
const apps = (await readdir(messagesDir, { withFileTypes: true })).filter((e) => e.isDirectory()).map((e) => e.name).sort();
for (const app of apps) {
  const { errors, warnings, stats } = await lintApp({ messagesDir, app, srcDirs: [join(repo, app, 'src')] });
  for (const e of errors) console.error(`error: ${e}`);
  if (verbose) for (const w of warnings) console.warn(`warn: ${w}`);
  const cover = Object.entries(stats)
    .map(([lc, s]) => `${lc} ${s.reviewed}/${s.total} reviewed`)
    .join(', ');
  console.log(`i18n lint: ${app}: ${errors.length} errors, ${warnings.length} warnings (${cover})`);
  if (errors.length) failed = true;
}
process.exit(failed ? 1 : 0);
