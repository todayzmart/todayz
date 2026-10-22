import { vitestPreset } from '@todayz/config/vitest';
import { defineConfig } from 'vitest/config';

export default defineConfig(vitestPreset({ dom: true, setupFiles: ['./src/test/setup.ts'] }));
