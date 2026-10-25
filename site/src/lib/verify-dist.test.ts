// The post-build guard (scripts/verify-dist.mjs) must catch any remote asset and any unexpected script.
import { describe, expect, it } from 'vitest';
import { checkFile } from '../../scripts/verify-dist.mjs';
import { LOCALES, isMachineDraft } from '../i18n/index.ts';

const TURNSTILE = '<script src="https://challenges.cloudflare.com/turnstile/v0/api.js" async defer></script>';
const FORM = '<form data-lead-form></form>';

describe('verify-dist', () => {
  it.each([
    ['a.css', '@font-face{src:url(https://cdn.example.com/f.woff2)}'],
    ['a.css', "body{background:url('http://img.example.com/x.png')}"],
    ['a.css', 'i{background:url( "//cdn.example.com/x.svg" )}'],
    ['a.css', '@import "https://cdn.example.com/reset.css";'],
    ['index.html', '<img src="https://img.example.com/x.png">'],
    ['index.html', "<link rel=stylesheet href='//cdn.example.com/a.css'>"],
    ['index.html', '<img src=https://img.example.com/x.png alt="">'],
    ['index.html', '<img srcset="/a.png 1x, https://img.example.com/a@2x.png 2x">'],
    ['index.html', '<div style="background:url(https://img.example.com/x.png)"></div>'],
    ['index.html', '<link rel="preconnect" href="https://fonts.gstatic.com">'],
    ['index.html', '<img src="https://app.todayz.in/logo.png" alt="">'],
    ['index.html', '<a href="https://evil.example.com/">x</a>'],
    ['index.html', '<a href="https://app.todayz.in.evil.example.com/">x</a>'],
  ])('fails a remote reference in %s: %s', (rel, text) => {
    expect(checkFile(rel, text)).not.toEqual([]);
  });

  it.each([
    ['a.css', '@font-face{src:url(/_astro/figtree-latin-wght-normal.abc.woff2) format("woff2")}'],
    ['a.css', 'i{background:url(data:image/svg+xml;base64,AAAA)}'],
    ['index.html', '<link rel="canonical" href="https://todayz.in/">'],
    ['index.html', '<link rel="alternate" hreflang="hi" href="https://todayz.in/hi/">'],
    ['index.html', '<meta property="og:url" content="https://todayz.in/"><a href="/privacy">x</a>'],
    ['index.html', '<a href="#main">Skip</a><img srcset="/a.png 1x, /a@2x.png 2x" alt="">'],
    ['index.html', '<script type="application/ld+json">{"url":"https://todayz.in/"}</script>'],
    ['index.html', '<a href="https://app.todayz.in/">Open the app</a>'],
    ['index.html', '<a href="https://app.todayz.in">Open the app</a>'],
    ['index.html', '<a class="x" href="https://business.todayz.in/signup">Shops</a>'],
  ])('passes a local or allowed reference in %s: %s', (rel, text) => {
    expect(checkFile(rel, text)).toEqual([]);
  });

  it('allows the hashed island bundle and the Turnstile script on a lead-form page', () => {
    const html = `${FORM}${TURNSTILE}<script type="module" src="/_astro/LeadForm.astro_astro_type_script_index_0_lang.B3oHOV_K.js"></script>`;
    expect(checkFile('index.html', html)).toEqual([]);
    expect(checkFile('_astro/LeadForm.astro_astro_type_script_index_0_lang.B3oHOV_K.js', '')).toEqual([]);
  });

  it('fails Turnstile on a page without the lead form', () => {
    expect(checkFile('about/index.html', TURNSTILE)).toEqual([
      'about/index.html: Turnstile script on a page without the lead form',
    ]);
  });

  it('fails other Turnstile URLs, other remote scripts and inline code', () => {
    const other = `${FORM}<script src="https://challenges.cloudflare.com/turnstile/v0/other.js"></script>`;
    expect(checkFile('index.html', other).length).toBeGreaterThan(0);
    expect(checkFile('index.html', '<script src="https://cdn.example.com/a.js"></script>').length).toBeGreaterThan(0);
    expect(checkFile('index.html', '<script src="/_astro/unhashed.js"></script>').length).toBeGreaterThan(0);
    expect(checkFile('index.html', '<script src="//challenges.cloudflare.com/turnstile/v0/api.js"></script>').length).toBeGreaterThan(0);
    expect(checkFile('index.html', '<script type="module">1</script>')).toHaveLength(1);
    expect(checkFile('index.html', '<script type="application/ld+json" src="/_astro/x.abcdef12.js"></script>')).toEqual([]);
  });

  it('fails JS that is not a hashed bundle under /_astro/', () => {
    expect(checkFile('page.js', '')).toEqual(['page.js: client JS emitted']);
    expect(checkFile('_astro/page.js', '')).toEqual(['_astro/page.js: client JS emitted']);
    expect(checkFile('app.mjs', '')).toEqual(['app.mjs: client JS emitted']);
  });

  it.each([
    '<button onclick="go()">x</button>',
    '<img src="/a.png" alt="" ONERROR=alert(1)>',
    "<body onload='x()'>",
    '<svg><a xlink:href="#" onmouseover="x()"></a></svg>',
  ])('fails an inline event handler: %s', (html) => {
    expect(checkFile('index.html', html).join('\n')).toMatch(/inline event handler/);
  });

  it.each([
    '<a href="javascript:alert(1)">x</a>',
    '<a href=" JavaScript:void(0)">x</a>',
    '<a href="java\tscript:x()">x</a>',
    '<a href="&#106;avascript:x()">x</a>',
    '<a href="&#x6A;avascript&colon;x()">x</a>',
    '<form action="javascript:x()"></form>',
    '<button formaction="javascript:x()">x</button>',
    '<iframe src="javascript:x()"></iframe>',
  ])('fails a javascript: URL: %s', (html) => {
    expect(checkFile('index.html', html).join('\n')).toMatch(/javascript: URL/);
  });

  it('does not mistake text that mentions on= or javascript for code', () => {
    expect(checkFile('index.html', '<p title="turn on = yes" data-note="javascript is off">on=1 javascript:</p>')).toEqual([]);
    expect(checkFile('index.html', '<a href="/javascript-guide/">x</a><input name="onboarding">')).toEqual([]);
    expect(checkFile('index.html', '<a href="&#99999999;">x</a>')).toEqual([]);
  });

  describe('lead form build settings', () => {
    const form = (key: string, api: string) => `<form data-lead-form data-api="${api}"></form><div class="cf-turnstile" data-sitekey="${key}"></div>`;

    it.each(['1x00000000000000000000AA', '2x00000000000000000000AB', '1x00000000000000000000BB', '3x00000000000000000000FF'])(
      'refuses the Turnstile test key %s outside e2e builds',
      (key) => {
        expect(checkFile('index.html', form(key, 'https://api.todayz.in')).join('\n')).toMatch(/test site key/);
        expect(checkFile('index.html', form(key, 'https://api.todayz.in'), { e2e: true })).toEqual([]);
      },
    );

    it.each(['https://api.todayz.localhost', 'https://api.todayz.localhost:8443/', 'http://localhost:8180', 'https://127.0.0.1'])(
      'refuses the local API %s outside e2e builds',
      (api) => {
        expect(checkFile('index.html', form('0x4AAAAAAA-real', api)).join('\n')).toMatch(/local API origin/);
        expect(checkFile('index.html', form('0x4AAAAAAA-real', api), { e2e: true })).toEqual([]);
      },
    );

    it('passes a real key and API', () => {
      expect(checkFile('index.html', form('0x4AAAAAAA-real', 'https://api.todayz.in'))).toEqual([]);
    });
  });

  describe('machine-draft locales never ship', () => {
    const drafts = ['hi', 'ta'];
    const prod = { draftLocales: drafts };
    it.each([
      ['hi/index.html', '<p>x</p>', /page of draft locale hi/],
      ['ta/for-retailers/index.html', '<p>x</p>', /page of draft locale ta/],
      ['og/home-hi.png', '', /OG image of draft locale hi/],
      ['index.html', '<link rel="alternate" hreflang="hi" href="https://todayz.in/hi/">', /hreflang for draft locale hi/],
      ['index.html', '<a href="/ta/" lang="ta-IN">தமிழ்</a>', /lang attribute for draft locale ta/],
      ['index.html', '<a href="/hi/for-retailers/">x</a>', /link for draft locale hi/],
      ['sitemap.xml', '<loc>https://todayz.in/hi/</loc>', /link for draft locale hi/],
    ])('refuses %s outside e2e builds', (rel, text, problem) => {
      expect(checkFile(rel, text, prod).join('\n')).toMatch(problem);
      expect(checkFile(rel, text, { ...prod, e2e: true })).toEqual([]);
    });
    it('passes English pages and reviewed locales', () => {
      expect(checkFile('index.html', '<html lang="en-IN"><a href="/for-retailers/" hreflang="en">x</a><a href="/hindi/">y</a></html>', prod)).toEqual([]);
      expect(checkFile('kn/index.html', '<html lang="kn-IN"></html>', prod)).toEqual([]);
      expect(checkFile('og/home.png', '', prod)).toEqual([]);
    });
    it('treats no locale as a draft since W4-05 approved them', () => {
      expect(LOCALES.filter((l) => isMachineDraft(l))).toEqual([]);
    });
  });
});
