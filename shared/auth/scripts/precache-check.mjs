#!/usr/bin/env node
// SC-GS-09: after `vite build`, every JS and CSS chunk in dist/assets must appear in the
// service worker's precache manifest (dist/sw.js), so a running page never asks the network for a
// chunk of its own version. Usage: node precache-check.mjs <app dir>   (default: cwd)
import { readdirSync, readFileSync, existsSync } from 'node:fs';
import { join, resolve } from 'node:path';

const root = resolve(process.argv[2] ?? '.');
const dist = join(root, 'dist');
const swPath = join(dist, 'sw.js');
const assets = join(dist, 'assets');
if (!existsSync(swPath) || !existsSync(assets)) {
  console.error(`precache-check: ${existsSync(swPath) ? 'dist/assets' : 'dist/sw.js'} is missing in ${root}; run the build first`);
  process.exit(1);
}
const sw = readFileSync(swPath, 'utf8');
const chunks = readdirSync(assets).filter((f) => /\.(js|css)$/.test(f));
const missing = chunks.filter((f) => !sw.includes(`assets/${f}`));
if (chunks.length === 0) {
  console.error('precache-check: no chunks found in dist/assets');
  process.exit(1);
}
if (missing.length > 0) {
  console.error(`precache-check: ${missing.length} of ${chunks.length} chunks are not precached:`);
  for (const f of missing) console.error(`  assets/${f}`);
  process.exit(1);
}
console.log(`precache-check: all ${chunks.length} chunks are in the precache manifest`);
