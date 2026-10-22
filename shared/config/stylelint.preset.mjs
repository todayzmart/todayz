// @todayz/config/stylelint: colours only through tokens (W0-05, 02-architecture.md §4.1).
//
//   // customer/stylelint.config.mjs
//   export { default } from '@todayz/config/stylelint';
//
// Raw hex, named colours and colour functions are errors everywhere except shared/tokens, which
// is generated from design/source/tokens.json and is the one place colour values live.

import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..');

/** Colour functions that would let a raw colour in through the back door. */
export const COLOR_FUNCTIONS = ['rgb', 'rgba', 'hsl', 'hsla', 'hwb', 'lab', 'lch', 'oklab', 'oklch', 'color'];

/** @type {import('stylelint').Config} */
const config = {
  // Absolute path: stylelint resolves `extends` from the app's folder, which does not list the package.
  extends: [fileURLToPath(import.meta.resolve('stylelint-config-standard'))],
  ignoreFiles: [join(repoRoot, 'shared', 'tokens', '**'), '**/shared/tokens/**', '**/dist/**', '**/node_modules/**', '**/coverage/**'],
  rules: {
    'color-no-hex': true,
    'color-named': 'never',
    'function-disallowed-list': COLOR_FUNCTIONS,
    // Keep the design system's naming: tz- prefixed classes, CSS modules use camelCase.
    'selector-class-pattern': null,
    'custom-property-pattern': null,
    'import-notation': 'string',
    'declaration-property-value-no-unknown': true,
    // CSS modules: :global(.tz-…) reaches design-system classes from a module.
    'selector-pseudo-class-no-unknown': [true, { ignorePseudoClasses: ['global'] }],
  },
};

export default config;
