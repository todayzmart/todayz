// Renders one 1200x630 Open Graph image per page and language into public/og/ (committed).
// Run after `pnpm build` when copy changes: `pnpm og`. It serves dist/ locally, opens each built page
// to read its own h1 and description, and draws a card with the site's tokens and self-hosted fonts,
// so Hindi, Kannada, Telugu and Tamil render in their real fonts. No network.
import { chromium } from '@playwright/test';
import { createServer } from 'node:http';
import { mkdirSync, readFileSync, statSync } from 'node:fs';
import { extname, join, normalize } from 'node:path';

const root = join(import.meta.dirname, '..');
const dist = join(root, 'dist');
const out = join(root, 'public', 'og');

const { allRoutes, pagePath } = await import('../src/lib/pages.ts');

const TYPES = { '.html': 'text/html', '.css': 'text/css', '.woff2': 'font/woff2', '.png': 'image/png', '.js': 'text/javascript' };

const server = createServer((req, res) => {
  const url = new URL(req.url ?? '/', 'http://x');
  let file = normalize(join(dist, decodeURIComponent(url.pathname)));
  if (!file.startsWith(dist)) return void res.writeHead(403).end();
  try {
    if (statSync(file).isDirectory()) file = join(file, 'index.html');
    res.writeHead(200, { 'content-type': TYPES[extname(file)] ?? 'application/octet-stream' }).end(readFileSync(file));
  } catch {
    res.writeHead(404).end();
  }
});
await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
const base = `http://127.0.0.1:${server.address().port}`;

mkdirSync(out, { recursive: true });
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1200, height: 630 }, deviceScaleFactor: 1 });
await page.route('**/*', (route) => (route.request().url().startsWith(base) ? route.continue() : route.abort()));

for (const { page: def, locale } of allRoutes()) {
  const name = `${def.id}${locale === 'en' ? '' : `-${locale}`}`;
  await page.goto(`${base}${pagePath(def, locale)}`);
  await page.evaluate(() => document.fonts.ready);
  await page.evaluate(() => {
    const title = document.querySelector('h1')?.textContent?.trim() ?? document.title;
    const lang = document.documentElement.lang;
    document.body.innerHTML = '';
    document.body.setAttribute('style', 'margin:0;display:block;min-height:0');
    const card = document.createElement('div');
    card.lang = lang;
    card.setAttribute(
      'style',
      [
        'box-sizing:border-box',
        'width:1200px',
        'height:630px',
        'padding:72px',
        'display:flex',
        'flex-direction:column',
        'justify-content:space-between',
        'background:linear-gradient(135deg,var(--brand-soft),var(--bg) 70%)',
        'font-family:var(--font-sans)',
        'color:var(--ink)',
      ].join(';'),
    );
    const brand = document.createElement('div');
    brand.setAttribute('style', 'display:flex;align-items:center;gap:20px;font-family:var(--font-display);font-weight:700;font-size:44px;color:var(--brand-strong)');
    const mark = document.createElement('img');
    mark.src = '/icons/mark-96.png';
    mark.setAttribute('style', 'width:72px;height:72px');
    brand.append(mark, document.createTextNode('TodayZ'));
    const h = document.createElement('div');
    h.textContent = title;
    h.setAttribute('style', 'font-family:var(--font-display);font-weight:700;font-size:76px;line-height:1.3;letter-spacing:0;max-width:1000px');
    const foot = document.createElement('div');
    foot.textContent = 'todayz.in';
    foot.setAttribute('style', 'font-size:32px;color:var(--ink-2)');
    card.append(brand, h, foot);
    document.body.append(card);
  });
  await page.evaluate(() => Promise.all([...document.images].map((i) => i.decode().catch(() => undefined))));
  await page.screenshot({ path: join(out, `${name}.png`), clip: { x: 0, y: 0, width: 1200, height: 630 } });
  console.log(`og: ${name}.png`);
}

await browser.close();
server.close();
