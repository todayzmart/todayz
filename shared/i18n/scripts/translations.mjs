#!/usr/bin/env node
// Translation workflow helper (see docs/translation-workflow.md).
//
//   node scripts/translations.mjs todo <app> <lc>                      JSON of keys that need a (new) translation
//   node scripts/translations.mjs import <app> <area> <lc> <file.json> add machine drafts from { key: message }
//   node scripts/translations.mjs approve <app> <area> <lc> <key...>   mark drafts reviewed (a human did this)
import { existsSync } from 'node:fs';
import { readFile, writeFile } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { TRANSLATED_LOCALES, argSignature, enHash, loadEnglish, loadTranslations } from './catalogue.mjs';
import { parse } from '@formatjs/icu-messageformat-parser';

const messagesDir = join(resolve(dirname(fileURLToPath(import.meta.url)), '..'), 'messages');

const sig = (m) => [...argSignature(parse(m, { ignoreTag: false, requiresOtherClause: true }))].sort().join(',');

/** Keys with no translation, or whose English changed since: [{ key, en, description, area, was? }]. */
export async function todo(dir, app, locale) {
  const english = await loadEnglish(dir, app);
  const tr = await loadTranslations(dir, app, locale);
  const out = [];
  for (const [key, e] of Object.entries(english)) {
    const t = tr[key];
    if (t && t.enHash === enHash(e.message)) continue;
    out.push({ key, area: e.file.replace(/\.en\.json$/, ''), en: e.message, description: e.description, ...(t ? { was: t.message } : {}) });
  }
  return out;
}

async function readArea(file) {
  return existsSync(file) ? JSON.parse(await readFile(file, 'utf8')) : {};
}
const writeArea = (file, data) => writeFile(file, JSON.stringify(data, null, 2) + '\n');

/** Writes machine drafts into <area>.<lc>.json. Throws on unknown keys or mismatched arguments. */
export async function importDrafts(dir, app, area, locale, drafts) {
  if (!TRANSLATED_LOCALES.includes(locale)) throw new Error(`unknown language ${locale}`);
  const english = await loadEnglish(dir, app);
  const file = join(dir, app, `${area}.${locale}.json`);
  const data = await readArea(file);
  for (const [key, message] of Object.entries(drafts)) {
    const e = english[key];
    if (!e || e.file !== `${area}.en.json`) throw new Error(`${key} is not in ${app}/${area}.en.json`);
    if (sig(message) !== sig(e.message)) throw new Error(`${key}: arguments differ from English`);
    data[key] = { message, review: 'machine', enHash: enHash(e.message) };
  }
  await writeArea(file, data);
  return Object.keys(drafts).length;
}

/** Marks drafts reviewed. Refuses stale entries: re-import them against the current English first. */
export async function approve(dir, app, area, locale, keys) {
  const english = await loadEnglish(dir, app);
  const file = join(dir, app, `${area}.${locale}.json`);
  const data = await readArea(file);
  for (const key of keys) {
    const t = data[key];
    if (!t) throw new Error(`${key}: no translation to approve`);
    if (!english[key] || t.enHash !== enHash(english[key].message)) throw new Error(`${key}: English changed, translate again first`);
    t.review = 'reviewed';
  }
  await writeArea(file, data);
  return keys.length;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const [cmd, app, ...rest] = process.argv.slice(2);
  if (cmd === 'todo') console.log(JSON.stringify(await todo(messagesDir, app, rest[0]), null, 2));
  else if (cmd === 'import') {
    const [area, lc, file] = rest;
    console.log(`imported ${await importDrafts(messagesDir, app, area, lc, JSON.parse(await readFile(file, 'utf8')))} machine drafts`);
  } else if (cmd === 'approve') {
    const [area, lc, ...keys] = rest;
    console.log(`approved ${await approve(messagesDir, app, area, lc, keys)} keys`);
  } else {
    console.error('usage: translations.mjs todo|import|approve ...');
    process.exit(2);
  }
}
