// Post-build guard for the static site: zero client JS, no font CDN, no remote assets.
// Runs after `astro build` (package.json "build") and exits non-zero on any violation.
// The rules are exported so src/lib/verify-dist.test.ts can prove each one bites.
import { readFileSync, readdirSync, realpathSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

/** Canonical and Open Graph links point at the public site; nothing else may be remote. */
const PUBLIC_ORIGIN = 'https://todayz.in/';

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

/** Every src, href, srcset, poster, data and action attribute value in HTML, quoted or not. */
function htmlUrls(text) {
  const urls = [];
  for (const m of text.matchAll(/\s(src|href|srcset|poster|data|action)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))/gi)) {
    const value = m[2] ?? m[3] ?? m[4] ?? '';
    if (m[1]?.toLowerCase() === 'srcset') urls.push(...value.split(',').map((c) => c.trim().split(/\s+/)[0] ?? ''));
    else urls.push(value);
  }
  return urls;
}

/**
 * Problems in one built file.
 * @param {string} rel   path relative to dist/
 * @param {string} text  file contents (only read for .html and .css)
 * @returns {string[]}
 */
export function checkFile(rel, text) {
  const problems = [];
  if (/\.m?js$/.test(rel)) problems.push(`${rel}: client JS emitted`);
  if (!/\.(html|css)$/.test(rel)) return problems;
  if (/fonts\.(googleapis|gstatic)\.com/.test(text)) problems.push(`${rel}: references a font CDN`);
  for (const url of cssUrls(text)) {
    if (isRemote(url)) problems.push(`${rel}: remote CSS url ${url}`);
  }
  if (rel.endsWith('.html')) {
    for (const m of text.matchAll(/<script\b([^>]*)>/g)) {
      if (!/type="application\/ld\+json"/.test(m[1] ?? '')) problems.push(`${rel}: <script${m[1]}>`);
    }
    for (const url of htmlUrls(text)) {
      if (isRemote(url) && !url.startsWith(PUBLIC_ORIGIN)) problems.push(`${rel}: remote reference ${url}`);
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
  console.log(`site: dist ok (${files} files, zero client JS, no remote assets)`);
}
