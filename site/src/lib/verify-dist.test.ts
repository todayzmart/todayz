// The post-build guard (scripts/verify-dist.mjs) must catch any remote asset, not just font CDNs.
import { describe, expect, it } from 'vitest';
import { checkFile } from '../../scripts/verify-dist.mjs';

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
  ])('fails a remote reference in %s: %s', (rel, text) => {
    expect(checkFile(rel, text)).not.toEqual([]);
  });

  it.each([
    ['a.css', '@font-face{src:url(/_astro/figtree-latin-wght-normal.abc.woff2) format("woff2")}'],
    ['a.css', 'i{background:url(data:image/svg+xml;base64,AAAA)}'],
    ['index.html', '<link rel="canonical" href="https://todayz.in/">'],
    ['index.html', '<meta property="og:url" content="https://todayz.in/"><a href="/privacy">x</a>'],
    ['index.html', '<a href="#main">Skip</a><img srcset="/a.png 1x, /a@2x.png 2x" alt="">'],
    ['index.html', '<script type="application/ld+json">{"url":"https://todayz.in/"}</script>'],
  ])('passes a local or allowed reference in %s: %s', (rel, text) => {
    expect(checkFile(rel, text)).toEqual([]);
  });

  it('fails client JS and any script that is not JSON-LD', () => {
    expect(checkFile('_astro/page.abc.js', '')).toEqual(['_astro/page.abc.js: client JS emitted']);
    expect(checkFile('index.html', '<script type="module">1</script>')).toHaveLength(1);
  });
});
