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
import ts from 'typescript';

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

/** Argument types that format a value inside the message. Amounts, dates and distances arrive pre-formatted. */
const SELF_FORMATTING = new Set([TYPE.number, TYPE.date, TYPE.time]);

function selfFormatting(ast, acc = []) {
  for (const el of ast) {
    if (SELF_FORMATTING.has(el.type)) acc.push(el.value);
    else if (el.type === TYPE.tag) selfFormatting(el.children, acc);
    else if (el.type === TYPE.plural || el.type === TYPE.select) {
      for (const o of Object.values(el.options)) selfFormatting(o.value, acc);
    }
  }
  return acc;
}

const parseIcu = (msg) => parse(msg, { ignoreTag: false, requiresOtherClause: true });
const sig = (msg) => [...argSignature(parseIcu(msg))].sort().join(',');

/** Why a message is unusable (parse error, number/date/time argument), or undefined. */
export function messageProblem(msg) {
  let ast;
  try {
    ast = parseIcu(msg);
  } catch (err) {
    return `does not parse: ${err.message}`;
  }
  const bad = selfFormatting(ast);
  if (bad.length) return `formats {${bad.join('}, {')}} itself; pass it pre-formatted (formatMoney, formatDistance, formatTime)`;
  return undefined;
}

/**
 * Whether a translation can ship: it parses, has no number/date/time argument and takes the same
 * arguments and tags as English. Returns undefined when fine, otherwise the reason.
 */
export function translationProblem(enMessage, message) {
  const own = messageProblem(message);
  if (own) return own;
  const want = sig(enMessage);
  const got = sig(message);
  return got === want ? undefined : `arguments differ from English (${got || 'none'} vs ${want || 'none'})`;
}

const KEY_SHAPE = /^[A-Za-z][A-Za-z0-9_-]*(?:\.[A-Za-z0-9_-]+)+$/;
const FILE_EXT = new Set(['css', 'scss', 'ts', 'tsx', 'js', 'jsx', 'mjs', 'cjs', 'json', 'svg', 'png', 'jpg', 'jpeg', 'webp', 'ico', 'html', 'xml', 'txt', 'md', 'astro', 'webmanifest', 'woff', 'woff2', 'map']);

/** A string that looks like a message key of one of the given namespaces. */
function looksLikeKey(text, namespaces) {
  if (!KEY_SHAPE.test(text)) return false;
  const parts = text.split('.');
  return namespaces.has(parts[0]) && !FILE_EXT.has(parts[parts.length - 1].toLowerCase());
}

function calleeName(expr) {
  if (ts.isIdentifier(expr)) return expr.text;
  if (ts.isPropertyAccessExpression(expr)) return expr.name.text;
  return undefined;
}
const strOf = (n) => (n && (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) ? n.text : undefined);

function idOfDescriptor(obj) {
  if (!obj || !ts.isObjectLiteralExpression(obj)) return undefined;
  for (const p of obj.properties) {
    if (ts.isPropertyAssignment(p) && (ts.isIdentifier(p.name) || ts.isStringLiteral(p.name)) && p.name.text === 'id') return strOf(p.initializer);
  }
  return undefined;
}

/**
 * Message ids used in one file.
 *   explicit    ids in formatMessage({ id }), defineMessage({ id }), defineMessages({ a: { id } }),
 *               <FormattedMessage id="..." />, and t('...'): checked whatever they look like
 *   candidates  any other string literal shaped like a key (`ns.part.part`) whose first segment is one
 *               of `namespaces` (the first segments in that app's catalogue, e.g. "shell"), so
 *               `label: 'shell.nav.serch'` held in data is caught. Import/export paths and names that
 *               end in a file extension are skipped; test files are not scanned at all.
 * Ids built at run time cannot be seen. .ts/.tsx/.js/.jsx/.mjs files are parsed with the TypeScript
 * parser (so `values={{ b: (c) => <b>{c}</b> }}` and nested braces are no problem); .astro files are
 * scanned for quoted strings only.
 */
export function extractMessageRefs(code, fileName = 'x.tsx', namespaces = new Set()) {
  const explicit = new Set();
  const candidates = new Set();
  if (fileName.endsWith('.astro')) {
    for (const m of code.matchAll(/\bt\(\s*(['"`])([A-Za-z0-9_.-]+)\1/g)) explicit.add(m[2]);
    for (const m of code.matchAll(/(['"`])([A-Za-z0-9_.-]+)\1/g)) if (looksLikeKey(m[2], namespaces)) candidates.add(m[2]);
    return { explicit, candidates };
  }
  const kind = /\.(tsx|jsx)$/.test(fileName) ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
  const sf = ts.createSourceFile(fileName, code, ts.ScriptTarget.Latest, true, kind);
  const visit = (node) => {
    if (ts.isCallExpression(node)) {
      const name = calleeName(node.expression);
      const first = node.arguments[0];
      if (name === 'formatMessage' || name === 'defineMessage') {
        const id = idOfDescriptor(first);
        if (id) explicit.add(id);
      } else if (name === 'defineMessages' && first && ts.isObjectLiteralExpression(first)) {
        for (const p of first.properties) {
          const id = ts.isPropertyAssignment(p) ? idOfDescriptor(p.initializer) : undefined;
          if (id) explicit.add(id);
        }
      } else if (name === 't') {
        const id = strOf(first);
        if (id) explicit.add(id);
      }
    } else if (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) {
      if (node.tagName.getText(sf) === 'FormattedMessage') {
        for (const a of node.attributes.properties) {
          if (ts.isJsxAttribute(a) && a.name.getText(sf) === 'id' && a.initializer) {
            const init = a.initializer;
            const id = strOf(init) ?? (ts.isJsxExpression(init) ? strOf(init.expression) : undefined);
            if (id) explicit.add(id);
          }
        }
      }
    } else if ((ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) && looksLikeKey(node.text, namespaces)) {
      const p = node.parent;
      if (!(ts.isImportDeclaration(p) || ts.isExportDeclaration(p) || ts.isExternalModuleReference(p) || ts.isLiteralTypeNode(p))) candidates.add(node.text);
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return { explicit, candidates };
}

/** Explicit and candidate ids together. */
export function extractMessageIds(code, fileName = 'x.tsx', namespaces = new Set()) {
  const { explicit, candidates } = extractMessageRefs(code, fileName, namespaces);
  return new Set([...explicit, ...candidates]);
}

async function* walk(dir) {
  for (const e of await readdir(dir, { withFileTypes: true })) {
    if (e.name === 'node_modules' || e.name === 'gen' || e.name.startsWith('.') || /\.gen\.ts$/.test(e.name)) continue;
    const p = join(dir, e.name);
    if (e.isDirectory()) yield* walk(p);
    else if (/\.(tsx?|jsx?|mjs|astro)$/.test(e.name) && !/\.d\.ts$/.test(e.name) && !/\.(test|spec)\.[tj]sx?$/.test(e.name)) yield p;
  }
}

/**
 * Every message id used under the given folders: id -> first file that uses it. A folder that does
 * not exist, or holds no source file, is a problem: the lint must never pass because it looked at
 * nothing. Returns { used, problems }.
 */
export async function scanUsage(srcDirs, namespaces = new Set()) {
  const used = new Map();
  const problems = [];
  for (const dir of srcDirs) {
    if (!existsSync(dir)) {
      problems.push(`source folder ${dir} does not exist`);
      continue;
    }
    let files = 0;
    for await (const file of walk(dir)) {
      files++;
      for (const id of extractMessageIds(await readFile(file, 'utf8'), file, namespaces)) if (!used.has(id)) used.set(id, file);
    }
    if (files === 0) problems.push(`source folder ${dir} holds no source files`);
  }
  return { used, problems };
}

/**
 * Lints one app's catalogues.
 * Errors (fail the build): English message that does not parse or formats a number/date/time itself;
 * id used in code but not in English (the missing-key lint; see extractMessageRefs for what counts as
 * use); a missing or empty source folder when `srcDirs` is given; translation of an unknown key;
 * translation that does not parse, has a number/date/time argument or takes other arguments than
 * English; bad review flag; empty message.
 * Warnings (reported, never fail): English keys never seen in code; keys not translated yet; machine
 * drafts waiting for review; stale translations (English changed since the translation; these skip the
 * argument check, they will be translated again anyway).
 * `srcDirs` omitted: the code is not scanned (library use). The CLI always passes it.
 * Returns { errors, warnings, stats } with stats[locale] = { total, reviewed, machine, missing, stale }.
 */
export async function lintApp({ messagesDir, app, srcDirs, locales = TRANSLATED_LOCALES }) {
  const errors = [];
  const warnings = [];
  const stats = {};
  const english = await loadEnglish(messagesDir, app);
  for (const [key, e] of Object.entries(english)) {
    const problem = messageProblem(e.message);
    if (problem) errors.push(`${app}/${e.file}: ${key}: English message ${problem}`);
  }
  if (srcDirs) {
    const namespaces = new Set(Object.keys(english).map((k) => k.split('.')[0]));
    const { used, problems } = await scanUsage(srcDirs, namespaces);
    for (const p of problems) errors.push(`${app}: ${p}`);
    if (!srcDirs.length) errors.push(`${app}: no source folder given`);
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
      if (t.enHash !== enHash(english[key].message)) {
        st.stale++;
        warnings.push(`${where}: English changed since this was translated (${locale}); translate and review again`);
        continue;
      }
      const problem = translationProblem(english[key].message, t.message);
      if (problem) {
        errors.push(`${where}: ${problem}`);
        continue;
      }
      if (t.review === 'reviewed') st.reviewed++;
      else {
        st.machine++;
        warnings.push(`${where}: machine draft, not reviewed (${locale})`);
      }
    }
    st.missing = st.total - Object.keys(tr).filter((k) => k in english).length;
    stats[locale] = st;
    if (st.missing) warnings.push(`${app}: ${st.missing} of ${st.total} keys not translated to ${locale} (English is shown)`);
  }
  return { errors, warnings, stats };
}
