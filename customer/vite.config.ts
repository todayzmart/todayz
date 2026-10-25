import { intlNoParserAlias, todayzServer } from '@todayz/config/vite';
import { authDefines } from '@todayz/auth/build';
import { pwaManifest, todayzIcons } from '@todayz/tokens/vite';
import { tanstackRouter } from '@tanstack/router-plugin/vite';
import react from '@vitejs/plugin-react';
import { defineConfig, loadEnv, type Plugin } from 'vite';
import { VitePWA } from 'vite-plugin-pwa';
import type * as ShareFn from './functions/x/[code]';

type ShareModule = typeof ShareFn;

// MapLibre's worker is copied into dist as an asset (`?url` import in @todayz/ui/map) with its own
// sourceMappingURL comment; shipped files must not point at maps (dist-check), so the comment is dropped.
function stripAssetSourceMapComments(): Plugin {
  const comment = /\n?\/\/[#@] sourceMappingURL=\S*\s*$/;
  return {
    name: 'tz-strip-asset-sourcemap-comments',
    apply: 'build',
    generateBundle(_options, bundle) {
      for (const file of Object.values(bundle)) {
        if (file.type !== 'asset' || !/\.m?js$/.test(file.fileName)) continue;
        const text = typeof file.source === 'string' ? file.source : Buffer.from(file.source).toString('utf8');
        if (comment.test(text)) file.source = text.replace(comment, '\n');
      }
    },
  };
}

// `/x/{code}` share links (W3-16): in dev the Pages Function's renderShare runs as middleware, so link
// previews behave as in production. Anything it cannot render falls through to the app's `/x/$code` route.
function shareLinks(apiBase: string): Plugin {
  return {
    name: 'tz-share-links',
    apply: 'serve',
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const m = /^\/x\/([0-9A-Za-z]{10})(?:\?|$)/.exec(req.url ?? '');
        if (req.method !== 'GET' || !m?.[1]) return next();
        const code = m[1];
        const origin = `https://${req.headers.host ?? 'app.todayz.localhost'}`;
        server
          .ssrLoadModule('/functions/x/[code].ts')
          .then((mod) => (mod as ShareModule).renderShare(code, origin, apiBase.replace(/\/$/, '')))
          .then(async (r) => {
            if (!r) return next();
            res.statusCode = r.status;
            r.headers.forEach((v, k) => res.setHeader(k, v));
            res.end(await r.text());
          })
          .catch(() => next());
      });
    },
  };
}

// Customer PWA: app.todayz.in, locally https://app.todayz.localhost (Caddy) -> 127.0.0.1:5301.
// A production build needs a real VITE_TURNSTILE_SITE_KEY and an https VITE_API_URL (authDefines throws
// without them); development, test and the e2e build fall back to the local API and Cloudflare's test key.
export default defineConfig(({ mode }) => ({
  ...todayzServer({ port: 5301, hostname: 'app.todayz.localhost' }),
  define: authDefines(mode, loadEnv(mode, process.cwd(), 'VITE_')),
  resolve: { alias: intlNoParserAlias },
  plugins: [
    tanstackRouter({ target: 'react', autoCodeSplitting: true }),
    react(),
    todayzIcons(),
    stripAssetSourceMapComments(),
    shareLinks(loadEnv(mode, process.cwd(), 'VITE_')['VITE_API_URL'] ?? ''),
    VitePWA({
      registerType: 'prompt',
      injectRegister: false,
      // No service worker in dev; it caches aggressively and hides HMR changes.
      devOptions: { enabled: false },
      manifest: pwaManifest({
        name: 'TodayZ',
        shortName: 'TodayZ',
        description: 'Find what local shops have in stock, book it, and pick it up or get it delivered.',
        orientation: 'portrait',
      }),
      workbox: {
        globPatterns: ['**/*.{js,mjs,css,html,png,svg}'],
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
