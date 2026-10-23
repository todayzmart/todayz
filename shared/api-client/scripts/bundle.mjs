// Redocly for the contract (W0-04). Two commands, both run by mk/25-contract.mk:
//
//   node scripts/bundle.mjs bundle   TODAYZ_API_ROOT (backend/gen/api/root.yaml, written by
//                                    apigen root) -> TODAYZ_OPENAPI (backend/api/openapi.yaml)
//   node scripts/bundle.mjs lint     redocly lint on TODAYZ_OPENAPI
//
// Redocly stays in this package (D-100 D5: any folder may depend on shared/), and the
// backend tells it every path it needs through the environment: TODAYZ_API_ROOT,
// TODAYZ_OPENAPI and TODAYZ_REDOCLY_CONFIG (backend/api/redocly.yaml). There is no
// default pointing into a sibling folder, so this package never guesses where the
// backend is. Telemetry and the update check are off: make check runs with no network.
import { execFileSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join, resolve } from 'node:path';

/** The paths the backend passed; every one is required. */
export function paths(env = process.env) {
  const need = (name) => {
    const v = env[name];
    if (!v) throw new Error(`${name} is not set: make gen passes it (mk/25-contract.mk)`);
    return v;
  };
  const openapi = need('TODAYZ_OPENAPI');
  return {
    root: env.TODAYZ_API_ROOT ?? '',
    openapi,
    config: env.TODAYZ_REDOCLY_CONFIG ?? join(dirname(openapi), 'redocly.yaml'),
  };
}

/** The redocly CLI entry point of the pinned @redocly/cli. */
export function redoclyBin() {
  const require = createRequire(import.meta.url);
  const pkg = require.resolve('@redocly/cli/package.json');
  return join(dirname(pkg), 'bin', 'cli.js');
}

export function redocly(args, { stdio = 'inherit' } = {}) {
  return execFileSync(process.execPath, [redoclyBin(), ...args], {
    stdio,
    env: { ...process.env, REDOCLY_TELEMETRY: 'off', REDOCLY_SUPPRESS_UPDATE_NOTICE: 'true' },
  });
}

function main(argv) {
  const [cmd] = argv;
  const p = paths();
  const config = existsSync(p.config) ? ['--config', p.config] : [];
  switch (cmd) {
    case 'bundle':
      if (!p.root) throw new Error('TODAYZ_API_ROOT is not set: make gen passes it (mk/25-contract.mk)');
      if (!existsSync(p.root)) throw new Error(`bundle: ${p.root} missing (run apigen root first; make gen does)`);
      redocly(['bundle', p.root, '-o', p.openapi, ...config]);
      return;
    case 'lint':
      if (!existsSync(p.openapi)) throw new Error(`lint: ${p.openapi} missing (run make gen first)`);
      redocly(['lint', p.openapi, ...config]);
      return;
    default:
      throw new Error('usage: bundle.mjs bundle|lint (TODAYZ_API_ROOT, TODAYZ_OPENAPI, TODAYZ_REDOCLY_CONFIG)');
  }
}

if (process.argv[1] && resolve(process.argv[1]) === resolve(import.meta.filename)) {
  try {
    main(process.argv.slice(2));
  } catch (err) {
    console.error(`bundle.mjs: ${err instanceof Error ? err.message : String(err)}`);
    process.exit(1);
  }
}
