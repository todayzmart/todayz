// Vite helpers from @todayz/tokens: serve and emit the generated icons, and build the PWA manifest
// from tokens so the theme colours never drift from design/source/tokens.json.
import { readFileSync, readdirSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const generated = resolve(dirname(fileURLToPath(import.meta.url)), '..', 'generated');
export const iconsDir = join(generated, 'icons');

function readTokens() {
  try {
    return JSON.parse(readFileSync(join(generated, 'tokens.json'), 'utf8'));
  } catch {
    throw new Error('@todayz/tokens: generated/ is missing. Run `make gen` first.');
  }
}

/** Token values by name (without --), read from the generated tokens.json. */
export function tokenValue(name) {
  const v = readTokens()[name];
  if (v === undefined) throw new Error(`@todayz/tokens: unknown token ${name}`);
  return v;
}

/** Icon files the apps publish under /icons/. */
export function iconFiles() {
  return readdirSync(iconsDir).filter((f) => f.endsWith('.png'));
}

/** Head tags for the icons and the theme colour (light theme only). */
export function iconHeadTags() {
  return [
    { tag: 'link', attrs: { rel: 'icon', type: 'image/png', sizes: '32x32', href: '/icons/favicon-32.png' }, injectTo: 'head' },
    { tag: 'link', attrs: { rel: 'icon', type: 'image/png', sizes: '48x48', href: '/icons/favicon-48.png' }, injectTo: 'head' },
    { tag: 'link', attrs: { rel: 'apple-touch-icon', href: '/icons/apple-touch-icon.png' }, injectTo: 'head' },
    { tag: 'meta', attrs: { name: 'theme-color', content: tokenValue('bg') }, injectTo: 'head' },
  ];
}

/**
 * Serves /icons/* in dev and emits the same files into the build output, so apps need no
 * copied public/ assets and the icons always come from the bag mark. Also adds the favicon,
 * apple-touch-icon and theme-color tags to index.html.
 */
export function todayzIcons() {
  return {
    name: 'todayz-icons',
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const m = req.url?.match(/^\/icons\/([a-z0-9-]+\.png)(?:\?.*)?$/);
        if (!m || !iconFiles().includes(m[1])) return next();
        res.setHeader('Content-Type', 'image/png');
        res.setHeader('Cache-Control', 'no-cache');
        res.end(readFileSync(join(iconsDir, m[1])));
      });
    },
    transformIndexHtml() {
      return iconHeadTags();
    },
    generateBundle() {
      for (const f of iconFiles()) {
        this.emitFile({ type: 'asset', fileName: `icons/${f}`, source: readFileSync(join(iconsDir, f)) });
      }
    },
  };
}

/** Web app manifest for vite-plugin-pwa. Colours come from tokens (light theme only). */
export function pwaManifest({ name, shortName, description, startUrl = '/', scope = '/', display = 'standalone', orientation }) {
  const bg = tokenValue('bg');
  return {
    name,
    short_name: shortName,
    description,
    lang: 'en-IN',
    dir: 'ltr',
    start_url: startUrl,
    scope,
    display,
    ...(orientation ? { orientation } : {}),
    background_color: bg,
    theme_color: bg,
    icons: [
      { src: '/icons/icon-192.png', sizes: '192x192', type: 'image/png', purpose: 'any' },
      { src: '/icons/icon-512.png', sizes: '512x512', type: 'image/png', purpose: 'any' },
      { src: '/icons/maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
    ],
  };
}
