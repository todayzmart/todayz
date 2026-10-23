// Build checks on dist/: every internal link and #fragment resolves, every canonical and hreflang URL
// is a built page, every page URL ends in a slash (links, canonical, hreflang, og:url, sitemap), and
// every HTML file passes the HTML validator. Runs after verify-dist (package.json
// "build"); no network. The pure helpers are exported for src/lib/check-site.test.ts.
import { HtmlValidate } from 'html-validate/node';
import { existsSync, readFileSync, readdirSync, realpathSync, statSync } from 'node:fs';
import { join, posix, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const ORIGIN = 'https://todayz.in';

/** Links and ids of one HTML document. */
export function scan(html) {
  const links = [];
  for (const m of html.matchAll(/<(a|link|img|script|source)\b([^>]*)>/gi)) {
    const attrs = m[2] ?? '';
    const tag = (m[1] ?? '').toLowerCase();
    for (const a of attrs.matchAll(/(?:^|\s)(href|src)\s*=\s*(?:"([^"]*)"|'([^']*)')/gi)) {
      links.push({ tag, attr: (a[1] ?? '').toLowerCase(), url: a[2] ?? a[3] ?? '' });
    }
  }
  const ids = new Set([...html.matchAll(/\sid\s*=\s*(?:"([^"]*)"|'([^']*)')/gi)].map((m) => m[1] ?? m[2] ?? ''));
  return { links, ids };
}

/**
 * The path of an internal URL (site-relative or on the public origin), or null for anything else
 * (other origins, mailto:, tel:, data:, fragment-only links).
 */
export function internalPath(url) {
  if (url === '' || url.startsWith('#') || /^(mailto:|tel:|data:)/i.test(url)) return null;
  let rest = url;
  if (url === ORIGIN || url.startsWith(`${ORIGIN}/`) || url.startsWith(`${ORIGIN}#`) || url.startsWith(`${ORIGIN}?`)) rest = url.slice(ORIGIN.length) || '/';
  else if (/^(?:[a-z][a-z0-9+.-]*:|\/\/)/i.test(url)) return null;
  return rest.split(/[?#]/)[0] || '/';
}

/** True when an internal path names a page (no file extension in its last segment) without the trailing slash. */
export function missingSlash(path) {
  if (path.endsWith('/')) return false;
  const last = path.slice(path.lastIndexOf('/') + 1);
  return !last.includes('.');
}

/** Page URLs without a trailing slash in one HTML document: links, canonical, hreflang and og:url. */
export function slashProblems(rel, html) {
  const problems = [];
  const urls = scan(html)
    .links.filter((l) => l.attr === 'href')
    .map((l) => l.url);
  for (const m of html.matchAll(/<meta\b[^>]*\sproperty\s*=\s*"og:url"[^>]*>/gi)) {
    const content = /\scontent\s*=\s*"([^"]*)"/i.exec(m[0]);
    if (content) urls.push(content[1] ?? '');
  }
  for (const url of urls) {
    const path = internalPath(url);
    if (path !== null && missingSlash(path)) problems.push(`${rel}: page URL without trailing slash ${url}`);
  }
  return problems;
}

/** Sitemap <loc> and alternate hrefs without a trailing slash. */
export function sitemapSlashProblems(xml) {
  const urls = [...xml.matchAll(/<loc>([^<]*)<\/loc>|\shref="([^"]*)"/g)].map((m) => m[1] ?? m[2] ?? '');
  return urls.filter((u) => {
    const path = internalPath(u);
    return path === null || missingSlash(path);
  }).map((u) => `sitemap.xml: URL without trailing slash or off-site ${u}`);
}

/** The dist file a site-relative URL path maps to, or null when none exists. */
export function resolveInDist(dist, pathname) {
  const clean = decodeURIComponent(pathname.split('?')[0] ?? '');
  const candidates = [clean, posix.join(clean, 'index.html'), `${clean}.html`];
  for (const c of candidates) {
    const file = join(dist, c);
    if (existsSync(file) && statSync(file).isFile()) return file;
  }
  return null;
}

function walk(dir) {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    return statSync(p).isDirectory() ? walk(p) : [p];
  });
}

/** Broken internal links in a built dist/. */
export function checkLinks(dist) {
  const problems = [];
  const pages = new Map();
  for (const file of walk(dist).filter((f) => f.endsWith('.html'))) {
    pages.set(file, scan(readFileSync(file, 'utf8')));
  }
  for (const [file, { links }] of pages) {
    const rel = relative(dist, file);
    const route = `/${rel.replace(/index\.html$/, '').replace(/\.html$/, '')}`;
    for (const { url } of links) {
      if (url === '' || /^(mailto:|tel:|data:)/i.test(url)) continue;
      let target = url;
      if (url.startsWith(ORIGIN)) target = url.slice(ORIGIN.length) || '/';
      else if (/^(?:https?:)?\/\//i.test(url)) continue; // other origins are verify-dist's job
      const [path, fragment] = target.split('#');
      const pathname = path === '' ? route : path;
      const resolved = resolveInDist(dist, pathname ?? '/');
      if (!resolved) {
        problems.push(`${rel}: broken link ${url}`);
        continue;
      }
      if (fragment && resolved.endsWith('.html')) {
        const doc = pages.get(resolved);
        if (doc && !doc.ids.has(fragment)) problems.push(`${rel}: missing #${fragment} in ${url}`);
      }
    }
  }
  return problems;
}

const validator = new HtmlValidate({
  extends: ['html-validate:recommended'],
  rules: {
    // Astro scopes styles with data attributes and writes void tags as <meta />; neither is an error.
    'void-style': 'off',
    'no-trailing-whitespace': 'off',
    'no-inline-style': 'off',
    // The Turnstile script and JSON-LD are inline by design.
    'no-inline-script': 'off',
    // Tokens give 4.5:1 text; the validator cannot see CSS.
    'wcag/h30': 'off',
    // Embedded Turnstile and FAQ markup are checked by the e2e suite.
    'prefer-native-element': 'off',
    'long-title': 'off',
  },
});

/** HTML validator messages for one document. */
export async function validateHtml(rel, html) {
  const report = await validator.validateString(html, rel);
  return report.results.flatMap((r) => r.messages.map((m) => `${rel}:${m.line}:${m.column} ${m.ruleId}: ${m.message}`));
}

export async function checkSite(dist) {
  const problems = checkLinks(dist);
  for (const file of walk(dist).filter((f) => f.endsWith('.html'))) {
    const rel = relative(dist, file);
    const html = readFileSync(file, 'utf8');
    problems.push(...slashProblems(rel, html), ...(await validateHtml(rel, html)));
  }
  const sitemap = join(dist, 'sitemap.xml');
  if (existsSync(sitemap)) problems.push(...sitemapSlashProblems(readFileSync(sitemap, 'utf8')));
  else problems.push('sitemap.xml is missing');
  return problems;
}

if (process.argv[1] && realpathSync(process.argv[1]) === realpathSync(fileURLToPath(import.meta.url))) {
  const dist = join(import.meta.dirname, '..', 'dist');
  const problems = await checkSite(dist);
  if (problems.length) {
    console.error(`site: ${problems.length} link or HTML problem(s)\n  ${problems.join('\n  ')}`);
    process.exit(1);
  }
  console.log('site: links and HTML valid');
}
