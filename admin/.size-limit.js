// Initial JS budget (02-architecture.md §4.1): the entry script plus every chunk index.html
// preloads, gzip. Lazy route chunks, the pseudo-locale and web-vitals are not counted.
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const dist = join(import.meta.dirname, 'dist');
let html;
try {
  html = readFileSync(join(dist, 'index.html'), 'utf8');
} catch {
  throw new Error('size-limit: dist/index.html is missing. Run `pnpm build` first.');
}
const refs = (re) => [...html.matchAll(re)].map((m) => join(dist, m[1]));
const js = [
  ...refs(/<script[^>]+type="module"[^>]+src="\/([^"]+\.js)"/g),
  ...refs(/<link[^>]+rel="modulepreload"[^>]+href="\/([^"]+\.js)"/g),
];
const css = refs(/<link[^>]+rel="stylesheet"[^>]+href="\/([^"]+\.css)"/g);
if (js.length === 0) throw new Error('size-limit: no entry script found in dist/index.html');

export default [
  { name: 'admin: initial JS', path: js, limit: '250 KB', gzip: true },
  { name: 'admin: initial CSS', path: css, limit: '20 KB', gzip: true },
];
