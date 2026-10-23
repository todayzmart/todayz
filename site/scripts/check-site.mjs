// Build checks on dist/: every internal link and #fragment resolves, every canonical and hreflang URL
// is a built page, and every HTML file passes the HTML validator. Runs after verify-dist (package.json
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
    problems.push(...(await validateHtml(relative(dist, file), readFileSync(file, 'utf8'))));
  }
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
