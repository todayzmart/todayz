// TodayZ marketing site (todayz.in). Static output, zero client JS, the same tokens, tz- CSS and
// self-hosted fonts as the apps (@todayz/tokens). Telemetry and the update check are switched off
// by the env vars in package.json scripts (ASTRO_TELEMETRY_DISABLED, ASTRO_DISABLE_UPDATE_CHECK).
import { copyFile, mkdir } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { ALLOWED_HOSTS, todayzServer } from '@todayz/config/vite';
import { iconFiles, iconHeadTags, iconsDir, todayzIcons } from '@todayz/tokens/vite';
import { defineConfig } from 'astro/config';

const PORT = 5304;
const local = todayzServer({ port: PORT, hostname: 'todayz.localhost' });

const HEAD_ID = 'virtual:todayz-head';

/**
 * Serves /icons/* in dev, copies the generated icons into dist/icons on build, and exposes the
 * favicon and theme-color tags as `virtual:todayz-head`. The tags are computed here, in Node,
 * because @todayz/tokens/vite reads generated/ relative to itself and cannot be bundled.
 */
function todayzSiteIcons() {
  const { configureServer } = todayzIcons();
  const head = {
    name: 'todayz-head',
    resolveId: (id) => (id === HEAD_ID ? `\0${HEAD_ID}` : undefined),
    load: (id) => (id === `\0${HEAD_ID}` ? `export default ${JSON.stringify(iconHeadTags())};` : undefined),
  };
  return {
    name: 'todayz-site-icons',
    hooks: {
      'astro:config:setup': ({ updateConfig }) => {
        updateConfig({ vite: { plugins: [{ name: 'todayz-icons-dev', configureServer }, head] } });
      },
      'astro:build:done': async ({ dir }) => {
        const out = join(fileURLToPath(dir), 'icons');
        await mkdir(out, { recursive: true });
        await Promise.all(iconFiles().map((f) => copyFile(join(iconsDir, f), join(out, f))));
      },
    },
  };
}

export default defineConfig({
  site: 'https://todayz.in',
  output: 'static',
  trailingSlash: 'ignore',
  devToolbar: { enabled: false },
  integrations: [todayzSiteIcons()],
  server: { host: '127.0.0.1', port: PORT, allowedHosts: ALLOWED_HOSTS },
  vite: {
    server: { strictPort: true, hmr: local.server.hmr },
    preview: { strictPort: true },
    build: { assetsInlineLimit: 0 },
    // Astro's prerender bundle imports these bare. Under pnpm they are not resolvable from
    // site/dist, so Node walks up past the repo and can pick up an unrelated copy (seen with a
    // CommonJS cookie in the home folder). Bundling them keeps the build self-contained.
    environments: { prerender: { resolve: { noExternal: ['cookie', 'html-escaper', 'zod'] } } },
  },
});
