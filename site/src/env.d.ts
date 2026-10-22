/** Favicon, apple-touch-icon and theme-color tags from @todayz/tokens (see astro.config.mjs). */
declare module 'virtual:todayz-head' {
  const tags: readonly { tag: 'link' | 'meta'; attrs: Record<string, string> }[];
  export default tags;
}
