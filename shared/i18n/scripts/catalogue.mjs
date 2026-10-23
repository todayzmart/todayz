// Catalogue pipeline for @todayz/i18n (W1-03): reading translations, the missing-key and
// consistency lint, and the message-id extractor. Plain Node so `make gen` and lint need no build.
//
// Files, per app, per screen area (owned by the WP that owns the area):
//   messages/<app>/<area>.en.json   { "key": { "defaultMessage": "...", "description": "..." } }   source of truth
//   messages/<app>/<area>.<lc>.json { "key": { "message": "...", "review": "machine"|"reviewed", "enHash": "ab12cd34" } }
// where <lc> is hi, kn, te or ta. See docs/translation-workflow.md.
import { TYPE, parse } from '@formatjs/icu-messageformat-parser';
import { createHash } from 'node:crypto';
import { existsSync } from 'node:fs';
import { readFile, readdir } from 'node:fs/promises';
import { join } from 'node:path';

/** Languages that are translated (English is the source). Same list as LOCALES in src/locales.ts. */
export const TRANSLATED_LOCALES = ['hi', 'kn', 'te', 'ta'];
export const REVIEW_STATES = ['machine', 'reviewed'];

/** Short fingerprint of an English message: a translation whose enHash differs is stale. */
export const enHash = (msg) => createHash('sha1').update(msg).digest('hex').slice(0, 8);

async function areaFiles(dir, suffix) {
  if (!existsSync(dir)) return [];
  return (await readdir(dir)).filter((f) => f.endsWith(suffix)).sort();
}

/** English messages of one app: key -> { message, description, file }. Duplicate keys are an error. */
export async function loadEnglish(messagesDir, app) {
  const dir = join(messagesDir, app);
  const out = {};
  for (const file of await areaFiles(dir, '.en.json')) {
    const entries = JSON.parse(await readFile(join(dir, file), 'utf8'));
    for (const [key, entry] of Object.entries(entries)) {
      if (key in out) throw new Error(`${app}/${file}: duplicate key ${key}`);
      const message = typeof entry === 'string' ? entry : entry.defaultMessage;
      if (typeof message !== 'string') throw new Error(`${app}/${file}: ${key} has no defaultMessage`);
      out[key] = { message, description: typeof entry === 'string' ? '' : (entry.description ?? ''), file };
    }
  }
  return out;
}

/** Translations of one app in one language: key -> { message, review, enHash, file }. */
export async function loadTranslations(messagesDir, app, locale) {
  const dir = join(messagesDir, app);
  const out = {};
  for (const file of await areaFiles(dir, `.${locale}.json`)) {
    const entries = JSON.parse(await readFile(join(dir, file), 'utf8'));
    for (const [key, entry] of Object.entries(entries)) {
      if (key in out) throw new Error(`${app}/${file}: duplicate key ${key}`);
      out[key] = { message: entry?.message, review: entry?.review, enHash: entry?.enHash, file };
    }
  }
  return out;
}

/** Arguments a message needs from its caller: "name:type" for values, "<tag>" for rich tags, sorted. */
export function argSignature(ast, acc = new Set()) {
  for (const el of ast) {
    if (el.type === TYPE.literal || el.type === TYPE.pound) continue;
    if (el.type === TYPE.tag) {
      acc.add(`<${el.value}>`);
      argSignature(el.children, acc);
      continue;
    }
    acc.add(`${el.value}:${el.type}`);
    if (el.type === TYPE.plural || el.type === TYPE.select) {
      for (const o of Object.values(el.options)) argSignature(o.value, acc);
    }
  }
  return acc;
}

const sig = (msg) => [...argSignature(parse(msg, { ignoreTag: false, requiresOtherClause: true }))].sort().join(',');

/**
 * Ids used in source code: `formatMessage({ id: 'a.b' ...`, `<FormattedMessage id="a.b"`, and
 * `defineMessage({ id: 'a.b' ...`. Ids built at run time cannot be seen and are not checked.
 */
export function extractMessageIds(source) {
  const ids = new Set();
  const patterns = [
    /\b(?:formatMessage|defineMessage)\(\s*\{[^}]*?\bid:\s*(['"`])([A-Za-z0-9_.-]+)\1/g,
    /<FormattedMessage\b[^>]*?\bid=(?:\{)?(['"`])([A-Za-z0-9_.-]+)\1/g,
  ];
  for (const re of patterns) for (const m of source.matchAll(re)) ids.add(m[2]);
  return ids;
}

async function* walk(dir) {
  if (!existsSync(dir)) return;
  for (const e of await readdir(dir, { withFileTypes: true })) {
    if (e.name === 'node_modules' || e.name === 'gen' || e.name.startsWith('.')) continue;
    const p = join(dir, e.name);
    if (e.isDirectory()) yield* walk(p);
    else if (/\.(tsx?|jsx?|mjs|astro)$/.test(e.name) && !/\.d\.ts$/.test(e.name)) yield p;
  }
}

/** Every message id used under the given source folders: id -> first file that uses it. */
export async function scanUsage(srcDirs) {
  const used = new Map();
  for (const dir of srcDirs) {
    for await (const file of walk(dir)) {
      for (const id of extractMessageIds(await readFile(file, 'utf8'))) if (!used.has(id)) used.set(id, file);
    }
  }
  return used;
}

/**
 * Lints one app's catalogues.
 * Errors (fail the build): English message that does not parse; id used in code but not in English
 * (the missing-key lint); translation of an unknown key; translation that does not parse or does not
 * take the same arguments as English; bad review flag; empty message.
 * Warnings (reported, never fail): English keys never seen in code; keys not translated yet; machine
 * drafts waiting for review; stale translations (English changed since the translation).
 * Returns { errors, warnings, stats } with stats[locale] = { total, reviewed, machine, missing, stale }.
 */
export async function lintApp({ messagesDir, app, srcDirs = [], locales = TRANSLATED_LOCALES }) {
  const errors = [];
  const warnings = [];
  const stats = {};
  const english = await loadEnglish(messagesDir, app);
  const enSig = {};
  for (const [key, e] of Object.entries(english)) {
    try {
      enSig[key] = sig(e.message);
    } catch (err) {
      errors.push(`${app}/${e.file}: ${key}: English message does not parse: ${err.message}`);
    }
  }
  if (srcDirs.length) {
    const used = await scanUsage(srcDirs);
    for (const [id, file] of used) {
      if (!(id in english)) errors.push(`${app}: missing key ${id} (used in ${file}) is not in any ${app}/*.en.json`);
    }
    for (const key of Object.keys(english)) if (!used.has(key)) warnings.push(`${app}: ${key} is not referenced by a literal id in code (fine if built at run time)`);
  }
  for (const locale of locales) {
    const tr = await loadTranslations(messagesDir, app, locale);
    const st = { total: Object.keys(english).length, reviewed: 0, machine: 0, missing: 0, stale: 0 };
    for (const [key, t] of Object.entries(tr)) {
      const where = `${app}/${t.file}: ${key}`;
      if (!(key in english)) {
        errors.push(`${where}: key is not in English (removed or renamed?)`);
        continue;
      }
      if (typeof t.message !== 'string' || t.message.trim() === '') {
        errors.push(`${where}: empty message`);
        continue;
      }
      if (!REVIEW_STATES.includes(t.review)) {
        errors.push(`${where}: review must be one of ${REVIEW_STATES.join(', ')}`);
        continue;
      }
      try {
        if (key in enSig && sig(t.message) !== enSig[key]) {
          errors.push(`${where}: arguments differ from English (${sig(t.message) || 'none'} vs ${enSig[key] || 'none'})`);
          continue;
        }
      } catch (err) {
        errors.push(`${where}: does not parse: ${err.message}`);
        continue;
      }
      if (t.enHash !== enHash(english[key].message)) {
        st.stale++;
        warnings.push(`${where}: English changed since this was translated (${locale}); review again`);
      } else if (t.review === 'reviewed') st.reviewed++;
      else st.machine++;
      if (t.review === 'machine') warnings.push(`${where}: machine draft, not reviewed (${locale})`);
    }
    st.missing = st.total - Object.keys(tr).filter((k) => k in english).length;
    stats[locale] = st;
    if (st.missing) warnings.push(`${app}: ${st.missing} of ${st.total} keys not translated to ${locale} (English is shown)`);
  }
  return { errors, warnings, stats };
}
