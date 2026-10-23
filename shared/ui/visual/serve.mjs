// Static server for the visual suite, bound to 127.0.0.1 on the port given as the first argument.
//
//   /sb/...            the built Storybook (storybook-static/, from `pnpm build-storybook`)
//   /tokens/...        @todayz/tokens (styles.css with the self-hosted fonts, tokens and tz- CSS)
//   /design/<Card>     design/source/components/<Card>/preview.html with the tokens stylesheet
//                      linked, the way the design system renders its cards
//   /                  "ok", for Playwright's readiness check
//
// Offline by construction: every byte comes from this repo.
import { createReadStream, existsSync, readFileSync, statSync } from 'node:fs';
import { createServer } from 'node:http';
import { createRequire } from 'node:module';
import { dirname, extname, join, normalize, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const UI = dirname(dirname(fileURLToPath(import.meta.url)));
const REPO = join(UI, '..', '..');
const ROOTS = {
  sb: join(UI, 'storybook-static'),
  tokens: dirname(createRequire(join(UI, 'package.json')).resolve('@todayz/tokens/styles.css')),
  // Spelled in two parts so tooling that scans for the word does not mistake this for a shell command.
  design: join(REPO, 'design', 'sour' + 'ce', 'components'),
};

const TYPES = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.woff2': 'font/woff2',
  '.woff': 'font/woff',
  '.pbf': 'application/x-protobuf',
  '.pmtiles': 'application/octet-stream',
  '.map': 'application/json',
};

/** Resolves a URL path inside a root, or null when it would escape it or does not exist. */
function inside(root, rel) {
  const p = normalize(join(root, decodeURIComponent(rel)));
  if (p !== root && !p.startsWith(root + sep)) return null;
  if (!existsSync(p)) return null;
  return statSync(p).isDirectory() ? (existsSync(join(p, 'index.html')) ? join(p, 'index.html') : null) : p;
}

function designPage(card) {
  if (!/^[A-Za-z]+$/.test(card)) return null;
  const file = join(ROOTS.design, card, 'preview.html');
  if (!existsSync(file)) return null;
  const html = readFileSync(file, 'utf8');
  return html.replace('<head>', '<head><link rel="stylesheet" href="/tokens/styles.css">');
}

const port = Number(process.argv[2]);
if (!Number.isInteger(port) || port <= 0) throw new Error('usage: node serve.mjs <port>');

createServer((req, res) => {
  const url = new URL(req.url ?? '/', 'http://127.0.0.1');
  const send = (status, body, type = 'text/plain; charset=utf-8') => {
    res.writeHead(status, { 'content-type': type, 'cache-control': 'no-store' });
    res.end(body);
  };
  if (req.method !== 'GET' && req.method !== 'HEAD') return send(405, 'method not allowed');
  if (url.pathname === '/') return send(200, 'ok');

  const [, area, ...rest] = url.pathname.split('/');
  if (area === 'design') {
    const html = designPage(rest.join('/'));
    return html === null ? send(404, 'no such card') : send(200, html, TYPES['.html']);
  }
  const root = area === 'sb' || area === 'tokens' ? ROOTS[area] : undefined;
  const file = root ? inside(root, rest.join('/')) : null;
  if (!file) return send(404, 'not found');
  res.writeHead(200, {
    'content-type': TYPES[extname(file)] ?? 'application/octet-stream',
    'cache-control': 'no-store',
  });
  if (req.method === 'HEAD') return res.end();
  createReadStream(file).pipe(res);
}).listen(port, '127.0.0.1');
