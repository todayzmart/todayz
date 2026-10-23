#!/usr/bin/env node
// Compiles shared/i18n/messages/<app>/<area>.<locale>.json into ICU ASTs for react-intl (W0-05
// bootstrap, completed by W1-03).
//
//   compiled/<app>/en.json     every area of the app merged, as ASTs (no runtime parser needed)
//   compiled/<app>/en-XA.json  accented, bracketed and 40% longer
//   compiled/<app>/<lc>.json   hi, kn, te, ta: translated messages; English wherever a key is not
//                              translated, not reviewed or stale. `--drafts` also ships machine
//                              drafts (for local checks); release builds never do.
//
// Output is git-ignored and produced by `make gen`.
import { parse } from '@formatjs/icu-messageformat-parser';
import { mkdir, readdir, rm, writeFile } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { PSEUDO_LOCALE, pseudoAst } from '../src/pseudo.mjs';
import { TRANSLATED_LOCALES, enHash, loadEnglish, loadTranslations, translationProblem } from './catalogue.mjs';

const pkg = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const messagesDir = join(pkg, 'messages');
const out = join(pkg, 'compiled');

/** Reads every <area>.en.json of one app; a key defined in two areas is an error. */
export async function loadApp(app) {
  const english = await loadEnglish(messagesDir, app);
  return Object.fromEntries(Object.entries(english).map(([k, v]) => [k, v.message]));
}

/** English messages with the shippable translations of one language laid over them. */
export async function loadLocaleMessages(app, locale, { drafts = false, dir = messagesDir, onWarn = console.warn } = {}) {
  const english = await loadEnglish(dir, app);
  const tr = await loadTranslations(dir, app, locale);
  const out = {};
  for (const [key, e] of Object.entries(english)) {
    const t = tr[key];
    const wanted = t && typeof t.message === 'string' && t.enHash === enHash(e.message) && (t.review === 'reviewed' || (drafts && t.review === 'machine'));
    // A translation ships only if it parses and takes the same arguments and tags as English.
    // Otherwise English ships and gen warns: one bad translation never aborts the build.
    const problem = wanted ? translationProblem(e.message, t.message) : undefined;
    if (problem) onWarn(`i18n: ${app}/${t.file}: ${key} (${locale}) not shipped, English used: ${problem}`);
    out[key] = wanted && !problem ? t.message : e.message;
  }
  return out;
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
  const drafts = process.argv.includes('--drafts');
  await rm(out, { recursive: true, force: true });
  const apps = (await readdir(messagesDir, { withFileTypes: true })).filter((d) => d.isDirectory()).map((d) => d.name);
  let keys = 0;
  for (const app of apps.sort()) {
    const messages = await loadApp(app);
    keys += Object.keys(messages).length;
    await mkdir(join(out, app), { recursive: true });
    await writeFile(join(out, app, 'en.json'), JSON.stringify(compile(messages)));
    await writeFile(join(out, app, `${PSEUDO_LOCALE}.json`), JSON.stringify(compile(messages, { pseudo: true })));
    for (const lc of TRANSLATED_LOCALES) {
      await writeFile(join(out, app, `${lc}.json`), JSON.stringify(compile(await loadLocaleMessages(app, lc, { drafts }))));
    }
  }
  console.log(`i18n: ${apps.length} apps, ${keys} keys, locales en, ${TRANSLATED_LOCALES.join(', ')} + ${PSEUDO_LOCALE} -> shared/i18n/compiled`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main();
}
