// Writes dist/_headers (Cloudflare Pages) after `astro build`: a Content-Security-Policy plus the
// usual hardening headers for every path. connect-src is the API origin the built lead forms post to
// (read from their data-api attribute), so the policy always matches what was built. The pure
// builder is exported for src/lib/headers.test.ts.
import { readFileSync, readdirSync, realpathSync, statSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const TURNSTILE_ORIGIN = 'https://challenges.cloudflare.com';

/**
 * The CSP for the site. Styles allow 'unsafe-inline' because Astro inlines small stylesheets and the
 * landing mock-ups use style="--x" custom properties; scripts never do (verify-dist enforces it).
 * @param {string[]} apiOrigins  origins the lead form posts to
 */
export function contentSecurityPolicy(apiOrigins) {
  const connect = ["'self'", ...apiOrigins].join(' ');
  return [
    "default-src 'self'",
    `script-src 'self' ${TURNSTILE_ORIGIN}`,
    `frame-src ${TURNSTILE_ORIGIN}`,
    `connect-src ${connect}`,
    "img-src 'self' data:",
    "style-src 'self' 'unsafe-inline'",
    "font-src 'self'",
    "object-src 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    "frame-ancestors 'none'",
  ].join('; ');
}

/**
 * The _headers file body.
 * @param {string[]} apiOrigins
 */
export function headersFile(apiOrigins) {
  return [
    '/*',
    `  Content-Security-Policy: ${contentSecurityPolicy(apiOrigins)}`,
    '  X-Content-Type-Options: nosniff',
    '  Referrer-Policy: strict-origin-when-cross-origin',
    '  X-Frame-Options: DENY',
    '  Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=()',
    // Astro's hashed bundles never change under a name: cache them for a year.
    '/_astro/*',
    '  Cache-Control: public, max-age=31536000, immutable',
    '',
  ].join('\n');
}

/**
 * The distinct origins named by data-api on the built lead forms. Throws on a value that is not an
 * absolute https URL, so a bad PUBLIC_API_URL never reaches the CSP.
 * @param {string[]} htmlFiles  file contents
 */
export function apiOrigins(htmlFiles) {
  const origins = new Set();
  for (const html of htmlFiles) {
    for (const m of html.matchAll(/\sdata-api\s*=\s*"([^"]*)"/g)) {
      let url;
      try {
        url = new URL(m[1] ?? '');
      } catch {
        throw new Error(`site: lead form data-api is not a URL: ${m[1]}`);
      }
      if (url.protocol !== 'https:') throw new Error(`site: lead form data-api must be https: ${m[1]}`);
      origins.add(url.origin);
    }
  }
  return [...origins].sort();
}

function walk(dir) {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    return statSync(p).isDirectory() ? walk(p) : [p];
  });
}

if (process.argv[1] && realpathSync(process.argv[1]) === realpathSync(fileURLToPath(import.meta.url))) {
  const dist = join(import.meta.dirname, '..', 'dist');
  const html = walk(dist)
    .filter((f) => f.endsWith('.html'))
    .map((f) => readFileSync(f, 'utf8'));
  const origins = apiOrigins(html);
  if (origins.length === 0) {
    console.error('site: no lead form found in dist/, cannot set connect-src');
    process.exit(1);
  }
  writeFileSync(join(dist, '_headers'), headersFile(origins));
  console.log(`site: wrote _headers (connect-src ${origins.join(' ')})`);
}
