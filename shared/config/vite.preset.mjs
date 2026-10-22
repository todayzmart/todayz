// @todayz/config/vite: dev and preview server options shared by the Vite apps (W0-05).
//
// Locally the apps run behind Caddy, which terminates TLS on 443 for *.todayz.localhost
// (root CLAUDE.md, "Local hosts and ports"). The dev server listens on 127.0.0.1:<fixed port>
// with strictPort, accepts the *.todayz.localhost Host header, and points the HMR client at
// wss://<app host>:443 so hot reload goes through Caddy too.
//
// TODAYZ_HMR=direct switches HMR back to the dev server's own port, for opening the app at
// http://127.0.0.1:<port> without Caddy (for example before the local stack exists).

export const ALLOWED_HOSTS = ['.todayz.localhost', 'todayz.localhost', '127.0.0.1', 'localhost'];

/**
 * @param {{ port: number, hostname: string }} opts  fixed dev port and the app's local hostname
 */
export function todayzServer({ port, hostname }) {
  const direct = process.env.TODAYZ_HMR === 'direct';
  return {
    server: {
      host: '127.0.0.1',
      port,
      strictPort: true,
      allowedHosts: ALLOWED_HOSTS,
      hmr: direct ? true : { protocol: 'wss', host: hostname, clientPort: 443 },
    },
    preview: {
      host: '127.0.0.1',
      strictPort: true,
      allowedHosts: ALLOWED_HOSTS,
    },
  };
}

/**
 * Messages are precompiled to ICU ASTs by @todayz/i18n (make gen), so the apps never parse
 * ICU at runtime. This alias swaps the parser for its no-parser build (about 10 KB gzip less).
 */
export const intlNoParserAlias = [
  { find: /^@formatjs\/icu-messageformat-parser$/, replacement: '@formatjs/icu-messageformat-parser/no-parser.js' },
];

export default todayzServer;
