// Post-build guard for the static site. Runs after `astro build` (package.json "build") and exits
// non-zero on any violation. The rules are exported so src/lib/verify-dist.test.ts proves each bites.
//
// Allowed client code (and nothing else):
//   - same-origin hashed bundles under /_astro/ (the lead form's island script),
//   - the Cloudflare Turnstile script, and only on pages that hold the lead form,
//   - inline JSON-LD (data, not code).
// Allowed remote references: https://todayz.in/ (canonical, hreflang) and <a href> to the app origins.
// Never allowed: inline event handlers (on*= attributes) and javascript: URLs, which the CSP in
// _headers (scripts/headers.mjs) would block anyway.
// Unless run with --e2e (the `build:e2e` script), a built lead form must not carry a Turnstile test
// site key or a local (.localhost) API origin: those builds are for local runs and tests only.
import { readFileSync, readdirSync, realpathSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const PUBLIC_ORIGIN = 'https://todayz.in/';
const TURNSTILE_SRC = 'https://challenges.cloudflare.com/turnstile/v0/api.js';
/** Links (not subresources) may point at the product apps. */
const APP_ORIGINS = ['https://app.todayz.in', 'https://business.todayz.in'];
/** A page holds the lead form when it renders this marker. */
const LEAD_FORM_MARKER = 'data-lead-form';
/** Cloudflare's published Turnstile test site keys (always pass, always block, forced challenge…). */
const TURNSTILE_TEST_KEYS = /^[123]x0{20}(?:AA|AB|BB|FF)$/;

/** http(s):// or protocol-relative //host: anything the browser would fetch from another origin. */
const REMOTE = /^(?:https?:)?\/\//i;

function isRemote(url) {
  return REMOTE.test(url.trim());
}

/** Every url(…) and @import target in CSS (or in an HTML <style> / style="" attribute). */
function cssUrls(text) {
  const urls = [];
  for (const m of text.matchAll(/url\(\s*(['"]?)([^'")]+)\1\s*\)/gi)) urls.push(m[2] ?? '');
  for (const m of text.matchAll(/@import\s+(['"])([^'"]+)\1/gi)) urls.push(m[2] ?? '');
  return urls;
}

/** Every attribute of every start tag: names are read in order, so a value never looks like a name. */
function tags(text) {
  const out = [];
  for (const tag of text.matchAll(/<([a-z][a-z0-9-]*)\b([^>]*)>/gi)) {
    const attrs = [];
    for (const m of (tag[2] ?? '').matchAll(/([^\s"'=<>/]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+)))?/g)) {
      attrs.push({ name: (m[1] ?? '').toLowerCase(), value: m[2] ?? m[3] ?? m[4] ?? '' });
    }
    out.push({ name: (tag[1] ?? '').toLowerCase(), attrs });
  }
  return out;
}

const URL_ATTRS = new Set(['src', 'href', 'srcset', 'poster', 'data', 'action', 'formaction', 'xlink:href']);

/** Every URL-valued attribute in HTML, with the tag it sits on. */
function htmlUrls(text) {
  const out = [];
  for (const { name, attrs } of tags(text)) {
    for (const { name: attr, value } of attrs) {
      if (!URL_ATTRS.has(attr)) continue;
      if (attr === 'srcset') {
        for (const c of value.split(',')) out.push({ tag: name, attr, url: c.trim().split(/\s+/)[0] ?? '' });
      } else out.push({ tag: name, attr, url: value });
    }
  }
  return out;
}

/** Decode the character references a browser decodes in an attribute value (numeric, and the few named ones that matter here). */
function decodeRefs(value) {
  const char = (cp) => (cp > 0 && cp <= 0x10ffff ? String.fromCodePoint(cp) : '\ufffd');
  return value
    .replace(/&#x([0-9a-f]+);?/gi, (_, hex) => char(parseInt(hex, 16)))
    .replace(/&#(\d+);?/g, (_, dec) => char(Number(dec)))
    .replace(/&(colon|tab|newline);/gi, (_, n) => ({ colon: ':', tab: '\t', newline: '\n' })[n.toLowerCase()] ?? '');
}

/** True for a javascript: URL in any form a browser still runs (character references, case, tabs, newlines). */
function isScriptUrl(url) {
  // Drop C0 controls and spaces (browsers strip them at the ends and tabs and newlines anywhere).
  const squeezed = [...decodeRefs(url)].filter((c) => c.codePointAt(0) > 0x20).join('');
  return /^javascript:/i.test(squeezed);
}

/** Hashed bundle emitted by Vite under /_astro/. */
function isOwnBundle(url) {
  return /^\/_astro\/[\w.-]+\.[\w-]{6,}\.js$/.test(url);
}

/**
 * Problems in one built file.
 * @param {string} rel   path relative to dist/
 * @param {string} text  file contents (only read for .html and .css)
 * @param {{ e2e?: boolean }} [options]  e2e: allow the Turnstile test key and a local API (build:e2e)
 * @returns {string[]}
 */
export function checkFile(rel, text, options = {}) {
  const problems = [];
  if (/\.m?js$/.test(rel) && !/^_astro\/[\w.-]+\.[\w-]{6,}\.js$/.test(rel)) problems.push(`${rel}: client JS emitted`);
  if (!/\.(html|css)$/.test(rel)) return problems;
  if (/fonts\.(googleapis|gstatic)\.com/.test(text)) problems.push(`${rel}: references a font CDN`);
  for (const url of cssUrls(text)) {
    if (isRemote(url)) problems.push(`${rel}: remote CSS url ${url}`);
  }
  if (rel.endsWith('.html')) {
    const hasLeadForm = text.includes(LEAD_FORM_MARKER);
    for (const m of text.matchAll(/<script\b([^>]*)>/g)) {
      const attrs = m[1] ?? '';
      const src = /\ssrc\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))/i.exec(` ${attrs}`);
      const srcValue = src ? (src[1] ?? src[2] ?? src[3] ?? '') : null;
      if (/type="application\/ld\+json"/.test(attrs) && srcValue === null) continue;
      if (srcValue === TURNSTILE_SRC) {
        if (!hasLeadForm) problems.push(`${rel}: Turnstile script on a page without the lead form`);
        continue;
      }
      if (srcValue !== null && isOwnBundle(srcValue)) continue;
      problems.push(`${rel}: <script${attrs}>`);
    }
    for (const { name, attrs } of tags(text)) {
      for (const { name: attr, value } of attrs) {
        if (/^on/.test(attr)) problems.push(`${rel}: inline event handler ${attr} on <${name}>`);
        if (!options.e2e && attr === 'data-sitekey' && TURNSTILE_TEST_KEYS.test(value.trim())) {
          problems.push(`${rel}: Turnstile test site key ${value} (set PUBLIC_TURNSTILE_SITE_KEY; test keys only in build:e2e)`);
        }
        if (!options.e2e && attr === 'data-api' && /\.localhost(?::\d+)?(?:\/|$)|\/\/(?:localhost|127\.0\.0\.1)(?::\d+)?(?:\/|$)/i.test(value)) {
          problems.push(`${rel}: local API origin ${value} (set PUBLIC_API_URL; local origins only in build:e2e)`);
        }
      }
    }
    for (const { tag, attr, url } of htmlUrls(text)) {
      if (isScriptUrl(url)) {
        problems.push(`${rel}: javascript: URL in <${tag} ${attr}>`);
        continue;
      }
      if (!isRemote(url) || url.startsWith(PUBLIC_ORIGIN)) continue;
      if (tag === 'script' && url === TURNSTILE_SRC) continue;
      if (tag === 'a' && attr === 'href' && APP_ORIGINS.some((o) => url === o || url.startsWith(`${o}/`) || url.startsWith(`${o}#`) || url.startsWith(`${o}?`))) continue;
      problems.push(`${rel}: remote reference ${url}`);
    }
  }
  return problems;
}

function walk(dir) {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    return statSync(p).isDirectory() ? walk(p) : [p];
  });
}

/**
 * All problems in a built dist/ folder.
 * @param {string} dist
 * @param {{ e2e?: boolean }} [options]
 * @returns {{ files: number, problems: string[] }}
 */
export function checkDist(dist, options = {}) {
  const files = walk(dist);
  const problems = files.flatMap((file) => {
    const rel = relative(dist, file);
    return checkFile(rel, /\.(html|css)$/.test(rel) ? readFileSync(file, 'utf8') : '', options);
  });
  if (!files.some((f) => relative(dist, f) === 'index.html')) problems.push('dist/index.html is missing');
  return { files: files.length, problems };
}

if (process.argv[1] && realpathSync(process.argv[1]) === realpathSync(fileURLToPath(import.meta.url))) {
  const e2e = process.argv.includes('--e2e');
  const { files, problems } = checkDist(join(import.meta.dirname, '..', 'dist'), { e2e });
  if (problems.length) {
    console.error(`site: ${problems.length} problem(s) in dist/\n  ${problems.join('\n  ')}`);
    process.exit(1);
  }
  console.log(
    `site: dist ok (${files} files; only own hashed bundles and Turnstile on lead-form pages; no remote assets; no inline handlers${e2e ? '; e2e build: test key and local API allowed' : ''})`,
  );
}
