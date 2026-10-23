// Post-build guard for the static site. Runs after `astro build` (package.json "build") and exits
// non-zero on any violation. The rules are exported so src/lib/verify-dist.test.ts proves each bites.
//
// Allowed client code (and nothing else):
//   - same-origin hashed bundles under /_astro/ (the lead form's island script),
//   - the Cloudflare Turnstile script, and only on pages that hold the lead form,
//   - inline JSON-LD (data, not code).
// Allowed remote references: https://todayz.in/ (canonical, hreflang) and <a href> to the app origins.
import { readFileSync, readdirSync, realpathSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const PUBLIC_ORIGIN = 'https://todayz.in/';
const TURNSTILE_SRC = 'https://challenges.cloudflare.com/turnstile/v0/api.js';
/** Links (not subresources) may point at the product apps. */
const APP_ORIGINS = ['https://app.todayz.in', 'https://business.todayz.in'];
/** A page holds the lead form when it renders this marker. */
const LEAD_FORM_MARKER = 'data-lead-form';

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

/** Every src, href, srcset, poster, data and action attribute in HTML, with the tag it sits on. */
function htmlUrls(text) {
  const out = [];
  for (const tag of text.matchAll(/<([a-z][a-z0-9-]*)\b([^>]*)>/gi)) {
    const name = (tag[1] ?? '').toLowerCase();
    for (const m of (tag[2] ?? '').matchAll(/(?:^|\s)(src|href|srcset|poster|data|action)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))/gi)) {
      const attr = (m[1] ?? '').toLowerCase();
      const value = m[2] ?? m[3] ?? m[4] ?? '';
      if (attr === 'srcset') {
        for (const c of value.split(',')) out.push({ tag: name, attr, url: c.trim().split(/\s+/)[0] ?? '' });
      } else out.push({ tag: name, attr, url: value });
    }
  }
  return out;
}

/** Hashed bundle emitted by Vite under /_astro/. */
function isOwnBundle(url) {
  return /^\/_astro\/[\w.-]+\.[\w-]{6,}\.js$/.test(url);
}

/**
 * Problems in one built file.
 * @param {string} rel   path relative to dist/
 * @param {string} text  file contents (only read for .html and .css)
 * @returns {string[]}
 */
export function checkFile(rel, text) {
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
    for (const { tag, attr, url } of htmlUrls(text)) {
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
 * @returns {{ files: number, problems: string[] }}
 */
export function checkDist(dist) {
  const files = walk(dist);
  const problems = files.flatMap((file) => {
    const rel = relative(dist, file);
    return checkFile(rel, /\.(html|css)$/.test(rel) ? readFileSync(file, 'utf8') : '');
  });
  if (!files.some((f) => relative(dist, f) === 'index.html')) problems.push('dist/index.html is missing');
  return { files: files.length, problems };
}

if (process.argv[1] && realpathSync(process.argv[1]) === realpathSync(fileURLToPath(import.meta.url))) {
  const { files, problems } = checkDist(join(import.meta.dirname, '..', 'dist'));
  if (problems.length) {
    console.error(`site: ${problems.length} problem(s) in dist/\n  ${problems.join('\n  ')}`);
    process.exit(1);
  }
  console.log(`site: dist ok (${files} files; only own hashed bundles and Turnstile on lead-form pages; no remote assets)`);
}
