// dist/_headers (scripts/headers.mjs): the CSP must allow exactly the site, Turnstile and the lead API.
import { describe, expect, it } from 'vitest';
import { apiOrigins, contentSecurityPolicy, headersFile } from '../../scripts/headers.mjs';

const directives = (csp: string) => new Map(csp.split('; ').map((d) => [d.split(' ')[0], d.split(' ').slice(1)] as const));

describe('_headers', () => {
  it('allows scripts and frames only from the site and Turnstile, and fetches only to the API', () => {
    const csp = directives(contentSecurityPolicy(['https://api.todayz.in']));
    expect(csp.get('script-src')).toEqual(["'self'", 'https://challenges.cloudflare.com']);
    expect(csp.get('frame-src')).toEqual(['https://challenges.cloudflare.com']);
    expect(csp.get('connect-src')).toEqual(["'self'", 'https://api.todayz.in']);
    expect(csp.get('frame-ancestors')).toEqual(["'none'"]);
    expect(csp.get('object-src')).toEqual(["'none'"]);
    expect(csp.get('default-src')).toEqual(["'self'"]);
    // No inline or eval script, ever.
    expect(csp.get('script-src')?.join(' ')).not.toMatch(/unsafe/);
  });

  it('writes one rule for every path with the hardening headers', () => {
    const file = headersFile(['https://api.todayz.in']);
    expect(file.split('\n')[0]).toBe('/*');
    expect(file).toContain('  X-Content-Type-Options: nosniff\n');
    expect(file).toContain('  Referrer-Policy: strict-origin-when-cross-origin\n');
    expect(file).toContain('  X-Frame-Options: DENY\n');
    expect(file).toMatch(/^ {2}Content-Security-Policy: .*connect-src 'self' https:\/\/api\.todayz\.in;/m);
  });

  it('caches the hashed /_astro/ bundles for a year, and nothing else', () => {
    const rules = headersFile(['https://api.todayz.in']).split(/\n(?=\/)/);
    expect(rules).toHaveLength(2);
    expect(rules[0]).not.toContain('Cache-Control');
    expect(rules[1]).toBe('/_astro/*\n  Cache-Control: public, max-age=31536000, immutable\n');
  });

  it('reads the API origin from the built forms, once', () => {
    const page = (api: string) => `<form class="form" data-lead-form data-api="${api}" data-source="site:home:en"></form>`;
    expect(apiOrigins([page('https://api.todayz.in'), page('https://api.todayz.in/'), '<p>no form</p>'])).toEqual(['https://api.todayz.in']);
    expect(apiOrigins([page('https://api.todayz.localhost')])).toEqual(['https://api.todayz.localhost']);
  });

  it('refuses an API URL that is not absolute https', () => {
    expect(() => apiOrigins(['<form data-api="/v1"></form>'])).toThrow(/not a URL/);
    expect(() => apiOrigins(['<form data-api="http://api.todayz.in"></form>'])).toThrow(/https/);
  });
});
