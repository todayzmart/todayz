import { intlNoParserAlias, todayzServer } from '@todayz/config/vite';
import { authDefines } from '@todayz/auth/build';
import { pwaManifest, todayzIcons } from '@todayz/tokens/vite';
import { tanstackRouter } from '@tanstack/router-plugin/vite';
import react from '@vitejs/plugin-react';
import { defineConfig, loadEnv } from 'vite';
import { VitePWA } from 'vite-plugin-pwa';

// Admin console: admin.todayz.in, locally https://admin.todayz.localhost (Caddy) -> 127.0.0.1:5303.
// A production build needs a real VITE_TURNSTILE_SITE_KEY and an https VITE_API_URL (authDefines throws without them).
export default defineConfig(({ mode }) => ({
  ...todayzServer({ port: 5303, hostname: 'admin.todayz.localhost' }),
  define: authDefines(mode, loadEnv(mode, process.cwd(), 'VITE_')),
  resolve: { alias: intlNoParserAlias },
  plugins: [
    tanstackRouter({ target: 'react', autoCodeSplitting: true }),
    react(),
    todayzIcons(),
    VitePWA({
      registerType: 'prompt',
      injectRegister: false,
      // No service worker in dev; it caches aggressively and hides HMR changes.
      devOptions: { enabled: false },
      manifest: pwaManifest({
        name: 'TodayZ Admin',
        shortName: 'TodayZ Admin',
        description: 'Admin console for TodayZ operations.',
        orientation: 'any',
      }),
      workbox: {
        globPatterns: ['**/*.{js,css,html,png,svg}'],
        // The generated worker ships without a sourceMappingURL, like the app (dist-check).
        sourcemap: false,
        navigateFallback: '/index.html',
        // Fonts for five scripts total ~1 MB: cache each one the first time a page uses it
        // instead of precaching all of them on install.
        runtimeCaching: [
          {
            urlPattern: ({ sameOrigin, url }) => sameOrigin && url.pathname.endsWith('.woff2'),
            handler: 'CacheFirst',
            options: { cacheName: 'tz-fonts', expiration: { maxEntries: 40 } },
          },
        ],
      },
    }),
  ],
  build: {
    target: 'es2022',
    // Maps are built for error symbolication but never referenced from shipped files.
    sourcemap: 'hidden',
    reportCompressedSize: false,
  },
}));
