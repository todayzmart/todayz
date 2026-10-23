import type * as MapLibre from 'maplibre-gl';

export type MapLibreModule = typeof MapLibre;

let pending: Promise<MapLibreModule> | null = null;
let webglSupport: boolean | undefined;

/**
 * True when the browser can create a WebGL context (MapLibre needs one). Probed once per page:
 * browsers cap live WebGL contexts (about 16), so every map mount creating a throwaway context
 * would evict real ones. The probe's context is released straight away.
 */
export function hasWebGL(): boolean {
  if (webglSupport !== undefined) return webglSupport;
  try {
    const canvas = document.createElement('canvas');
    const ctx = canvas.getContext('webgl2') ?? canvas.getContext('webgl');
    ctx?.getExtension('WEBGL_lose_context')?.loseContext();
    webglSupport = Boolean(ctx);
  } catch {
    webglSupport = false;
  }
  return webglSupport;
}

/**
 * Loads MapLibre on demand (it is large and only the map screens need it) and registers the
 * pmtiles:// protocol once, so Protomaps basemaps load straight from the .pmtiles file.
 *
 * MapLibre 6 finds its worker at runtime, next to its own module (`new URL(..., import.meta.url)`
 * with a computed name). Bundlers cannot see that, so the worker file is never emitted and the
 * map fails with "Worker failed to load". The `?url` import makes Vite emit the (self-contained)
 * worker as an asset, and setWorkerUrl points MapLibre at it.
 */
export function loadMapLibre(): Promise<MapLibreModule> {
  pending ??= Promise.all([
    import('maplibre-gl'),
    import('pmtiles'),
    import('maplibre-gl/dist/maplibre-gl-worker.mjs?url'),
  ])
    .then(([ml, pm, worker]) => {
      const lib = ((ml as unknown as { default?: MapLibreModule }).default ?? ml) as MapLibreModule;
      lib.setWorkerUrl(worker.default);
      const protocol = new pm.Protocol();
      lib.addProtocol('pmtiles', protocol.tile);
      return lib;
    })
    .catch((err: unknown) => {
      pending = null;
      throw err;
    });
  return pending;
}
