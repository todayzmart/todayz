#!/usr/bin/env node
// Message lint. The caller says where each app's source is; this package never reads sibling
// folders on its own:
//
//   node scripts/lint.mjs --src customer=../../customer/src --src site=../../site/src ... [--verbose]
//
// Every app that has a catalogue needs a --src, and each folder must exist and hold source files,
// otherwise the lint fails (it must never pass by looking at nothing). The repo wiring is
// mk/42-i18n.mk. Exit 1 on any error; warnings (untranslated, machine drafts, unused) are counted,
// `--verbose` lists them.
import { readdir } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { lintApp } from './catalogue.mjs';

const pkg = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const messagesDir = join(pkg, 'messages');

const args = process.argv.slice(2);
const verbose = args.includes('--verbose');
const srcByApp = new Map();
for (let i = 0; i < args.length; i++) {
  if (args[i] !== '--src') continue;
  const [app, ...rest] = (args[++i] ?? '').split('=');
  const dir = rest.join('=');
  if (!app || !dir) {
    console.error('error: --src expects <app>=<folder>');
    process.exit(2);
  }
  srcByApp.set(app, [...(srcByApp.get(app) ?? []), resolve(dir)]);
}

let failed = false;
const apps = (await readdir(messagesDir, { withFileTypes: true })).filter((e) => e.isDirectory()).map((e) => e.name).sort();
for (const app of apps) {
  const srcDirs = srcByApp.get(app);
  if (!srcDirs) {
    console.error(`error: ${app}: no --src ${app}=<folder> given`);
    failed = true;
    continue;
  }
  const { errors, warnings, stats } = await lintApp({ messagesDir, app, srcDirs });
  for (const e of errors) console.error(`error: ${e}`);
  if (verbose) for (const w of warnings) console.warn(`warn: ${w}`);
  const cover = Object.entries(stats)
    .map(([lc, s]) => `${lc} ${s.reviewed}/${s.total} reviewed`)
    .join(', ');
  console.log(`i18n lint: ${app}: ${errors.length} errors, ${warnings.length} warnings (${cover})`);
  if (errors.length) failed = true;
}
for (const app of srcByApp.keys()) {
  if (!apps.includes(app)) {
    console.error(`error: --src names ${app}, which has no catalogue`);
    failed = true;
  }
}
process.exit(failed ? 1 : 0);
