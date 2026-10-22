#!/usr/bin/env node
// Compiles shared/i18n/messages/<app>/<area>.en.json into ICU ASTs for react-intl, plus the
// generated en-XA pseudo-locale (W0-05 bootstrap; W1-03 owns the full pipeline).
//
//   compiled/<app>/en.json     every area of the app merged, as ASTs (no runtime parser needed)
//   compiled/<app>/en-XA.json  accented, bracketed and 40% longer
//
// Output is git-ignored and produced by `make gen`.
import { parse } from '@formatjs/icu-messageformat-parser';
import { mkdir, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { PSEUDO_LOCALE, pseudoAst } from '../src/pseudo.mjs';

const pkg = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const messagesDir = join(pkg, 'messages');
const out = join(pkg, 'compiled');

/** Reads every <area>.en.json of one app; a key defined in two areas is an error. */
export async function loadApp(app) {
  const dir = join(messagesDir, app);
  const merged = {};
  for (const file of (await readdir(dir)).filter((f) => f.endsWith('.en.json')).sort()) {
    const entries = JSON.parse(await readFile(join(dir, file), 'utf8'));
    for (const [key, entry] of Object.entries(entries)) {
      if (key in merged) throw new Error(`${app}/${file}: duplicate key ${key}`);
      const msg = typeof entry === 'string' ? entry : entry.defaultMessage;
      if (typeof msg !== 'string') throw new Error(`${app}/${file}: ${key} has no defaultMessage`);
      merged[key] = msg;
    }
  }
  return merged;
}

export function compile(messages, { pseudo = false } = {}) {
  const outMsgs = {};
  for (const [key, msg] of Object.entries(messages)) {
    const ast = parse(msg, { ignoreTag: false, requiresOtherClause: true });
    outMsgs[key] = pseudo ? pseudoAst(ast) : ast;
  }
  return outMsgs;
}

async function main() {
  await rm(out, { recursive: true, force: true });
  const apps = (await readdir(messagesDir, { withFileTypes: true })).filter((d) => d.isDirectory()).map((d) => d.name);
  let keys = 0;
  for (const app of apps.sort()) {
    const messages = await loadApp(app);
    keys += Object.keys(messages).length;
    await mkdir(join(out, app), { recursive: true });
    await writeFile(join(out, app, 'en.json'), JSON.stringify(compile(messages)));
    await writeFile(join(out, app, `${PSEUDO_LOCALE}.json`), JSON.stringify(compile(messages, { pseudo: true })));
  }
  console.log(`i18n: ${apps.length} apps, ${keys} keys, locales en + ${PSEUDO_LOCALE} -> shared/i18n/compiled`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main();
}
