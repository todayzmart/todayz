// @todayz/config/vitest: shared Vitest options (W0-05). Merge into an app's vitest config:
//
//   import { defineConfig, mergeConfig } from 'vitest/config';
//   import { vitestPreset } from '@todayz/config/vitest';
//   export default mergeConfig(viteConfig, defineConfig(vitestPreset({ dom: true })));

/**
 * @param {{ dom?: boolean, setupFiles?: string[] }} [opts]
 */
export function vitestPreset({ dom = false, setupFiles = [] } = {}) {
  return {
    test: {
      environment: dom ? 'happy-dom' : 'node',
      include: ['src/**/*.test.{ts,tsx}', 'test/**/*.test.{ts,tsx,mjs}'],
      exclude: ['**/node_modules/**', '**/dist/**', '**/e2e/**', '**/fixtures/**'],
      setupFiles,
      restoreMocks: true,
      unstubEnvs: true,
      unstubGlobals: true,
      passWithNoTests: false,
      // No watch mode and no update checks in make check.
      watch: false,
      reporters: ['default'],
    },
  };
}

export default vitestPreset;
