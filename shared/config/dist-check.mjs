// @todayz/config/dist-check: post-build checks for a Vite app's dist/ (W0-05).
//
//   node ./node_modules/@todayz/config/dist-check.mjs dist
//
// Source maps are built as `sourcemap: 'hidden'`: the .map files exist for error symbolication,
// but shipped JS and CSS must not point browsers at them with a `sourceMappingURL` comment.
// That includes the Workbox service worker (VitePWA `workbox.sourcemap: false`).
import { readFileSync, readdirSync, realpathSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const SHIPPED = /\.(?:m?js|css)$/;
const MAP_COMMENT = /[#@]\s*sourceMappingURL=/;

/** Shipped JS/CSS files under `dir` (relative paths) that still carry a sourceMappingURL comment. */
export function sourceMapComments(dir) {
  return readdirSync(dir, { recursive: true, encoding: 'utf8' })
    .filter((f) => SHIPPED.test(f))
    .filter((f) => MAP_COMMENT.test(readFileSync(join(dir, f), 'utf8')))
    .map((f) => relative(dir, join(dir, f)));
}

// Apps run this through their node_modules symlink, so compare real paths.
if (process.argv[1] && realpathSync(process.argv[1]) === realpathSync(fileURLToPath(import.meta.url))) {
  const dir = resolve(process.argv[2] ?? 'dist');
  const bad = sourceMapComments(dir);
  if (bad.length > 0) {
    console.error(`dist-check: sourceMappingURL in shipped files (use build.sourcemap: 'hidden' and workbox.sourcemap: false):\n  ${bad.join('\n  ')}`);
    process.exit(1);
  }
  console.log(`dist-check: ${relative(process.cwd(), dir) || '.'} has no sourceMappingURL comments`);
}
