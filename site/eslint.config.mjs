import { todayzEslint } from '@todayz/config/eslint';

// .astro files are checked by `astro check` (types) and Stylelint (styles); ESLint covers the TS/JS.
export default todayzEslint({ pkgDir: import.meta.dirname, react: false, ignores: ['.astro/**'] });
