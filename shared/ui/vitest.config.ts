import react from '@vitejs/plugin-react';
import { vitestPreset } from '@todayz/config/vitest';
import { defineConfig } from 'vitest/config';

const preset = vitestPreset({ dom: true, setupFiles: ['./src/test/setup.ts'] });

// The React plugin is repeated here rather than importing vite.config: an extensionless TS import
// trips Vite's native config loader, and the base tsconfig forbids .ts extensions.
export default defineConfig({
  ...preset,
  plugins: [react()],
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
});
