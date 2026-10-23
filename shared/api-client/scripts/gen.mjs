// `make gen` for @todayz/api-client (W0-04): everything under src/gen and
// src/mocks/handlers.gen.ts from the bundled contract (TODAYZ_OPENAPI, default
// backend/api/openapi.yaml), all git-ignored.
//
//   src/gen/openapi.json          the bundle as JSON (the handlers and tests read it)
//   src/gen/<domain>/<domain>.ts  fetch client (orval, orval.config.ts)
//   src/gen/model/*.ts            types
//   src/gen/<domain>/<domain>.zod.ts
//   src/gen/problems.json         the problem codes of TODAYZ_PROBLEMS_DIR (backend/api/problems,
//                                 passed by mk/25-contract.mk; the tests read it, D-100 Low 13)
//   src/mocks/handlers.gen.ts     MSW handlers from the examples (scripts/msw.mjs)
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join, resolve } from 'node:path';

import { paths, redocly } from './bundle.mjs';
import { operations, renderHandlers } from './msw.mjs';

const pkgDir = resolve(import.meta.dirname, '..');
const gen = join(pkgDir, 'src', 'gen');
const handlersFile = join(pkgDir, 'src', 'mocks', 'handlers.gen.ts');

/** The problem codes of every *.yaml in dir (`- code: x` lines), sorted, at least one. */
function problemCodes(dir) {
  const codes = new Set();
  for (const f of readdirSync(dir)) {
    if (!f.endsWith('.yaml')) continue;
    for (const m of readFileSync(join(dir, f), 'utf8').matchAll(/^\s*-\s*code:\s*([a-z][a-z0-9_]*)\s*$/gm)) {
      if (m[1]) codes.add(m[1]);
    }
  }
  if (codes.size === 0) throw new Error(`${dir}: no problem codes found`);
  return [...codes].sort();
}

function main() {
  const { openapi } = paths();
  if (!existsSync(openapi)) throw new Error(`${openapi} missing: run make gen (apigen root, then bundle)`);
  const problemsDir = process.env.TODAYZ_PROBLEMS_DIR;
  if (!problemsDir) throw new Error('TODAYZ_PROBLEMS_DIR unset: make gen passes the problems directory (shared/ reads nothing from backend/ itself)');
  if (!existsSync(problemsDir)) throw new Error(`${problemsDir} missing (TODAYZ_PROBLEMS_DIR)`);
  const codes = problemCodes(problemsDir);

  rmSync(gen, { recursive: true, force: true });
  mkdirSync(gen, { recursive: true });
  const json = join(gen, 'openapi.json');
  redocly(['bundle', openapi, '-o', json], { stdio: 'pipe' });

  const require = createRequire(import.meta.url);
  const orval = join(dirname(require.resolve('orval/package.json')), 'dist', 'bin', 'orval.mjs');
  execFileSync(process.execPath, [orval, '--config', join(pkgDir, 'orval.config.ts')], {
    cwd: pkgDir,
    stdio: ['ignore', 'pipe', 'inherit'],
    env: { ...process.env, TODAYZ_OPENAPI: openapi },
  });

  writeFileSync(join(gen, 'problems.json'), JSON.stringify(codes, null, 2) + '\n');

  const doc = JSON.parse(readFileSync(json, 'utf8'));
  mkdirSync(dirname(handlersFile), { recursive: true });
  writeFileSync(handlersFile, renderHandlers(doc));
  const ops = operations(doc);
  const domains = new Set(ops.map((o) => o.path.split('/')[2] ?? 'root'));
  console.log(`api-client gen: ${ops.length} operations -> src/gen (${domains.size} prefixes), ${codes.length} problem codes, src/mocks/handlers.gen.ts`);
}

try {
  main();
} catch (err) {
  console.error(`gen.mjs: ${err instanceof Error ? err.message : String(err)}`);
  process.exit(1);
}
