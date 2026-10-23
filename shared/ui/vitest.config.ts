import { vitestPreset } from '@todayz/config/vitest';
import { defineConfig, mergeConfig } from 'vitest/config';
import viteConfig from './vite.config';

const preset = vitestPreset({ dom: true, setupFiles: ['./src/test/setup.ts'] });

export default mergeConfig(
  viteConfig,
  defineConfig({
    ...preset,
    test: {
      ...preset.test,
      // 08-testing: shared/ui coverage at least 90%.
      coverage: {
        provider: 'v8',
        include: ['src/**/*.{ts,tsx}'],
        exclude: ['src/**/*.stories.tsx', 'src/**/*.test.{ts,tsx}', 'src/test/**', 'src/**/index.ts', 'src/**/*.d.ts'],
        reporter: ['text-summary'],
        thresholds: { lines: 90, statements: 90, functions: 90, branches: 85 },
      },
    },
  }),
);
