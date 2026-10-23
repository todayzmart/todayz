import { todayzEslint } from '@todayz/config/eslint';

export default [
  ...todayzEslint({ pkgDir: import.meta.dirname }),
  {
    // A library of hooks and components, not an app: Fast Refresh boundaries do not apply.
    files: ['src/**'],
    rules: { 'react-refresh/only-export-components': 'off' },
  },
];
