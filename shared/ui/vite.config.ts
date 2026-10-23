import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// Used by Storybook (react-vite) and Vitest. The package itself ships TS source; apps bundle it.
export default defineConfig({
  plugins: [react()],
});
