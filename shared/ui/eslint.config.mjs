import { todayzEslint } from '@todayz/config/eslint';

export default [
  ...todayzEslint({ pkgDir: import.meta.dirname, ignores: ['storybook-static/**', 'visual/out/**'] }),
  {
    // Stories and tests export story objects and helpers, not only components; Fast Refresh
    // boundaries do not apply to them.
    files: ['src/**/*.stories.tsx', 'src/**/*.test.tsx', 'src/test/**'],
    rules: { 'react-refresh/only-export-components': 'off' },
  },
  {
    // The visual runner and its static server are Node scripts that report progress.
    files: ['visual/**'],
    rules: { 'no-console': 'off' },
  },
];
