// Proves the shared config bites: raw colours fail Stylelint and ESLint, sibling-app imports fail
// ESLint, the dist check finds source map comments, and the smoke helpers block remote requests.
import { ESLint } from 'eslint';
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import stylelint from 'stylelint';
import { describe, expect, it } from 'vitest';
import { sourceMapComments } from '../dist-check.mjs';
import { boundaryZones, repoRoot, todayzEslint } from '../eslint.preset.mjs';
import { guardPage, isLocalUrl, qaDir } from '../playwright.preset.mjs';
import stylelintConfig from '../stylelint.preset.mjs';
import { todayzServer } from '../vite.preset.mjs';
import { vitestPreset } from '../vitest.preset.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const fixture = (f) => join(here, '..', 'fixtures', f);

async function lintAs(fixtureName, appFolder, asFile) {
  const pkgDir = join(repoRoot, appFolder);
  const eslint = new ESLint({
    cwd: pkgDir,
    overrideConfigFile: true,
    overrideConfig: todayzEslint({ pkgDir, react: false }),
  });
  const [res] = await eslint.lintText(readFileSync(fixture(fixtureName), 'utf8'), {
    filePath: join(pkgDir, 'src', asFile),
  });
  return res.messages;
}

describe('eslint preset', () => {
  it('fails a relative import into a sibling app and an import of a sibling app package', async () => {
    const messages = await lintAs('sibling-import.ts', 'customer', 'sibling-import.ts');
    const rules = messages.map((m) => m.ruleId);
    expect(rules).toContain('import-x/no-restricted-paths');
    expect(rules).toContain('no-restricted-imports');
    expect(messages.every((m) => m.severity === 2)).toBe(true);
  });

  it('allows importing shared packages', async () => {
    const messages = await lintAs('shared-import.ts', 'customer', 'shared-import.ts');
    expect(messages).toEqual([]);
  });

  it('builds zones against every other app folder', () => {
    const zones = boundaryZones('business');
    expect(zones.map((z) => z.from)).toEqual(
      expect.arrayContaining(['./customer', './admin', './site', './backend', './e2e']),
    );
    expect(zones.some((z) => z.from === './business')).toBe(false);
    expect(boundaryZones('shared/tokens').every((z) => z.target === './shared/tokens')).toBe(true);
  });

  it('keeps fixtures out of the normal lint globs', async () => {
    const eslint = new ESLint({ cwd: resolve(here, '..'), overrideConfigFile: true, overrideConfig: todayzEslint({ pkgDir: resolve(here, '..'), react: false }) });
    expect(await eslint.isPathIgnored(fixture('sibling-import.ts'))).toBe(true);
  });
});

describe('stylelint preset', () => {
  it('fails raw hex, named colours and colour functions', async () => {
    const { results } = await stylelint.lint({ files: [fixture('raw-colours.css')], config: stylelintConfig });
    const rules = results[0].warnings.map((w) => w.rule);
    expect(rules).toEqual(expect.arrayContaining(['color-no-hex', 'color-named', 'function-disallowed-list']));
    expect(results[0].errored).toBe(true);
  });

  it('passes colours that come from tokens', async () => {
    const { results } = await stylelint.lint({ files: [fixture('tokens-only.css')], config: stylelintConfig });
    expect(results[0].warnings).toEqual([]);
  });

  it('lets shared/tokens hold raw values', async () => {
    const { results } = await stylelint.lint({
      code: ':root { --ink: #0c231f; }',
      codeFilename: join(repoRoot, 'shared', 'tokens', 'generated', 'tokens.css'),
      config: stylelintConfig,
    });
    expect(results[0]?.ignored ?? results.length === 0).toBe(true);
  });
});

describe('vite and vitest presets', () => {
  it('pins the port, accepts *.todayz.localhost and sends HMR through Caddy', () => {
    const prev = process.env.TODAYZ_HMR;
    delete process.env.TODAYZ_HMR;
    const { server } = todayzServer({ port: 5301, hostname: 'app.todayz.localhost' });
    expect(server).toMatchObject({ port: 5301, strictPort: true, host: '127.0.0.1' });
    expect(server.allowedHosts).toContain('.todayz.localhost');
    expect(server.hmr).toEqual({ protocol: 'wss', host: 'app.todayz.localhost', clientPort: 443 });
    process.env.TODAYZ_HMR = 'direct';
    expect(todayzServer({ port: 5301, hostname: 'app.todayz.localhost' }).server.hmr).toBe(true);
    if (prev === undefined) delete process.env.TODAYZ_HMR;
    else process.env.TODAYZ_HMR = prev;
  });

  it('uses happy-dom for DOM tests and never passes with no tests', () => {
    expect(vitestPreset({ dom: true }).test.environment).toBe('happy-dom');
    expect(vitestPreset().test.passWithNoTests).toBe(false);
  });
});

describe('raw colours in code', () => {
  it('fails hex, rgb() and hsl() string and template literals in an app', async () => {
    const messages = await lintAs('raw-colour-literal.ts', 'customer', 'raw-colour-literal.ts');
    const hits = messages.filter((m) => m.ruleId === 'no-restricted-syntax');
    expect(hits.map((m) => m.line)).toEqual([2, 3, 4, 5, 6]);
    expect(hits.every((m) => m.severity === 2 && /use a token/.test(m.message))).toBe(true);
  });

  it('passes token colours, fragment links and lookalikes', async () => {
    const messages = await lintAs('token-colours.ts', 'business', 'token-colours.ts');
    expect(messages).toEqual([]);
  });

  it('lets shared/tokens spell colours out', async () => {
    const messages = await lintAs('raw-colour-literal.ts', 'shared/tokens', 'raw-colour-literal.ts');
    expect(messages.filter((m) => m.ruleId === 'no-restricted-syntax')).toEqual([]);
  });
});

describe('dist-check', () => {
  const dist = (files) => {
    const dir = mkdtempSync(join(tmpdir(), 'tz-dist-'));
    for (const [name, body] of Object.entries(files)) {
      mkdirSync(dirname(join(dir, name)), { recursive: true });
      writeFileSync(join(dir, name), body);
    }
    return dir;
  };

  it('finds shipped JS and CSS that point at a source map', () => {
    const dir = dist({
      'assets/index-abc.js': 'console.log(1);\n//# sourceMappingURL=index-abc.js.map\n',
      'assets/index-abc.css': 'a{}\n/*# sourceMappingURL=index-abc.css.map */\n',
      'sw.js': 'self.x=1;\n//@ sourceMappingURL=sw.js.map\n',
      'assets/clean-def.js': 'console.log(2);\n',
      'assets/index-abc.js.map': '{"version":3,"file":"# sourceMappingURL=ignored"}',
    });
    expect(sourceMapComments(dir).sort()).toEqual(['assets/index-abc.css', 'assets/index-abc.js', 'sw.js']);
  });

  it('passes a hidden-sourcemap build (maps on disk, no comments)', () => {
    const dir = dist({ 'assets/index-abc.js': 'console.log(1);\n', 'assets/index-abc.js.map': '{}' });
    expect(sourceMapComments(dir)).toEqual([]);
  });
});

describe('playwright preset', () => {
  it.each([
    ['http://127.0.0.1:41234/assets/a.js', true],
    ['http://localhost:5301/', true],
    ['http://[::1]:8080/', true],
    ['https://app.todayz.localhost/', true],
    ['data:font/woff2;base64,AAAA', true],
    ['blob:http://127.0.0.1:41234/7c1e', true],
    ['https://fonts.googleapis.com/css2?family=Figtree', false],
    ['https://fonts.gstatic.com/s/figtree.woff2', false],
    ['https://todayz.in/', false],
    ['http://localhost.evil.example/', false],
    ['http://127.0.0.2/', false],
  ])('isLocalUrl(%s) is %s', (url, local) => {
    expect(isLocalUrl(url)).toBe(local);
  });

  function fakePage() {
    const listeners = {};
    let handler;
    return {
      route: async (_glob, fn) => {
        handler = fn;
      },
      on: (event, fn) => {
        listeners[event] = fn;
      },
      request(url) {
        const calls = [];
        handler({
          request: () => ({ url: () => url }),
          continue: () => calls.push('continue'),
          abort: (reason) => calls.push(`abort:${reason}`),
        });
        return calls;
      },
      emit: (event, arg) => listeners[event](arg),
    };
  }

  it('guardPage aborts and records non-local requests and collects errors and responses', async () => {
    const page = fakePage();
    const guard = await guardPage(page);
    expect(page.request('http://127.0.0.1:4000/assets/figtree-latin.woff2')).toEqual(['continue']);
    expect(page.request('https://fonts.googleapis.com/css2')).toEqual(['abort:blockedbyclient']);
    page.emit('console', { type: () => 'warning', text: () => 'ignored' });
    page.emit('console', { type: () => 'error', text: () => 'boom' });
    page.emit('pageerror', new Error('uncaught'));
    page.emit('response', { url: () => 'http://127.0.0.1:4000/', status: () => 200 });
    expect(guard).toEqual({
      external: ['https://fonts.googleapis.com/css2'],
      errors: ['console: boom', 'pageerror: uncaught'],
      responses: [{ url: 'http://127.0.0.1:4000/', status: 200 }],
    });
  });

  it('writes screenshots to test-results unless TZ_QA_DIR asks for the QA folder', () => {
    const prev = process.env.TZ_QA_DIR;
    delete process.env.TZ_QA_DIR;
    expect(qaDir()).toBe(join(process.cwd(), 'test-results', 'screenshots'));
    process.env.TZ_QA_DIR = 'docs/qa/W0-05';
    expect(qaDir()).toBe(join(repoRoot, 'docs', 'qa', 'W0-05'));
    process.env.TZ_QA_DIR = '/tmp/elsewhere';
    expect(qaDir()).toBe('/tmp/elsewhere');
    if (prev === undefined) delete process.env.TZ_QA_DIR;
    else process.env.TZ_QA_DIR = prev;
  });
});
