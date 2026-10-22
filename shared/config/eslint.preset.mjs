// @todayz/config/eslint: the flat ESLint config every TodayZ frontend package uses (W0-05).
//
//   // customer/eslint.config.mjs
//   import { todayzEslint } from '@todayz/config/eslint';
//   export default todayzEslint({ pkgDir: import.meta.dirname });
//
// Strict TypeScript, React hooks, jsx-a11y, and import rules that make a sibling-app import an
// error, so each app folder can become its own repo (root CLAUDE.md, "Folder map").
import js from '@eslint/js';
import { createNodeResolver, importX } from 'eslint-plugin-import-x';
import jsxA11y from 'eslint-plugin-jsx-a11y';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';
import globals from 'globals';
import { dirname, relative, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import tseslint from 'typescript-eslint';

/** Repo root (shared/config/.. /..). */
export const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..');

/** Top-level folders that will each become their own repo. None may import another. */
export const APP_FOLDERS = ['customer', 'business', 'admin', 'site', 'storefront', 'backend', 'e2e', 'infra'];

/** Workspace package names of the apps; importing one by name is just as wrong as a relative path. */
export const APP_PACKAGES = ['@todayz/customer', '@todayz/business', '@todayz/admin', '@todayz/site', '@todayz/storefront'];

export const DEFAULT_IGNORES = [
  '**/dist/**',
  '**/build/**',
  '**/coverage/**',
  '**/node_modules/**',
  '**/generated/**',
  '**/compiled/**',
  '**/.astro/**',
  '**/dev-dist/**',
  '**/test-results/**',
  '**/playwright-report/**',
  '**/routeTree.gen.ts',
  // Lint fixtures are meant to fail; their own tests lint them on purpose.
  '**/fixtures/**',
];

/**
 * Raw colours in TS/TSX string literals (`'#0f9d58'`, `` `rgb(0 0 0)` ``, `'hsl(…)'`). Colours come
 * from tokens (`var(--brand)`); only shared/tokens may spell one out. Stylelint covers the CSS.
 */
const RAW_COLOUR = String.raw`/(?:^|[^\w&])#(?:[0-9a-f]{3,4}|[0-9a-f]{6}|[0-9a-f]{8})(?![\w-])|\b(?:rgb|hsl)a?\(/i`;
const RAW_COLOUR_MESSAGE = 'No raw colours in code: use a token, e.g. var(--brand). Only shared/tokens defines colours.';
export const RAW_COLOUR_SELECTORS = [
  { selector: `Literal[value=${RAW_COLOUR}]`, message: RAW_COLOUR_MESSAGE },
  { selector: `TemplateElement[value.raw=${RAW_COLOUR}]`, message: RAW_COLOUR_MESSAGE },
];

/**
 * Zones for import-x/no-restricted-paths: code under `folder` may not import from any other app
 * folder, and shared/* may not import from any app.
 */
export function boundaryZones(folder) {
  const top = folder.split('/')[0];
  const others = APP_FOLDERS.filter((f) => f !== top);
  return others.map((from) => ({
    target: `./${folder}`,
    from: `./${from}`,
    message:
      top === 'shared'
        ? `shared/ must not import from an app (${from}/).`
        : `${top}/ must not import from the sibling app ${from}/. Import from shared/* or design/ only.`,
  }));
}

/**
 * @param {object} opts
 * @param {string} opts.pkgDir    absolute path of the package (use import.meta.dirname)
 * @param {boolean} [opts.react]  enable React, hooks and jsx-a11y rules (default true)
 * @param {string[]} [opts.ignores]
 */
export function todayzEslint({ pkgDir, react = true, ignores = [] }) {
  const folder = relative(repoRoot, pkgDir).split(sep).join('/');
  const files = ['**/*.{ts,tsx,mts,cts,js,jsx,mjs,cjs}'];
  const configs = [
    { ignores: [...DEFAULT_IGNORES, ...ignores] },
    { ...js.configs.recommended, files },
    ...tseslint.configs.strict.map((c) => ({ ...c, files })),
    {
      files,
      languageOptions: {
        ecmaVersion: 2023,
        sourceType: 'module',
        globals: { ...globals.browser, ...globals.node },
      },
      plugins: { 'import-x': importX },
      settings: {
        'import-x/resolver-next': [createNodeResolver({ extensions: ['.ts', '.tsx', '.mts', '.js', '.jsx', '.mjs', '.json'] })],
      },
      rules: {
        'import-x/no-restricted-paths': ['error', { basePath: repoRoot, zones: boundaryZones(folder) }],
        'import-x/no-relative-packages': 'error',
        'import-x/no-duplicates': 'error',
        'import-x/no-self-import': 'error',
        'import-x/no-cycle': ['error', { maxDepth: 3 }],
        'no-restricted-imports': [
          'error',
          {
            paths: APP_PACKAGES.map((name) => ({ name, message: 'Never import a sibling app; use shared/* instead.' })),
            patterns: [
              { group: APP_PACKAGES.map((p) => `${p}/*`), message: 'Never import a sibling app; use shared/* instead.' },
              { regex: '^https?://', message: 'No remote imports: everything is bundled or self-hosted.' },
            ],
          },
        ],
        '@typescript-eslint/consistent-type-imports': ['error', { fixStyle: 'inline-type-imports' }],
        '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
        eqeqeq: ['error', 'always'],
        'no-console': ['error', { allow: ['warn', 'error'] }],
      },
    },
    ...(folder.startsWith('shared/tokens') ? [] : [{ files: ['**/*.{ts,tsx,mts,cts}'], rules: { 'no-restricted-syntax': ['error', ...RAW_COLOUR_SELECTORS] } }]),
    {
      // Build scripts and configs may log.
      files: ['scripts/**', '**/*.config.{js,mjs,ts}', '**/*.preset.mjs', '**/dist-check.mjs'],
      rules: { 'no-console': 'off' },
    },
  ];
  if (react) {
    const jsx = ['**/*.{tsx,jsx}'];
    configs.push(
      { ...reactHooks.configs.flat['recommended-latest'], files },
      { ...jsxA11y.flatConfigs.strict, files: jsx },
      {
        files: ['src/**/*.tsx'],
        // Route files are code-split by TanStack Router (autoCodeSplitting), which handles their HMR.
        ignores: ['src/routes/**'],
        plugins: { 'react-refresh': reactRefresh },
        rules: { 'react-refresh/only-export-components': ['error', { allowConstantExport: true }] },
      },
    );
  }
  return configs;
}

export default todayzEslint;
