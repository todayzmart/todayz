// The post-build guard (scripts/verify-dist.mjs) must catch any remote asset and any unexpected script.
import { describe, expect, it } from 'vitest';
import { checkFile } from '../../scripts/verify-dist.mjs';

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
});
