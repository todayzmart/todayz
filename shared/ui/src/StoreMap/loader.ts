import type * as MapLibre from 'maplibre-gl';

export type MapLibreModule = typeof MapLibre;

let pending: Promise<MapLibreModule> | null = null;

/** True when the browser can create a WebGL context (MapLibre needs one). */
export function hasWebGL(): boolean {
  try {
    const canvas = document.createElement('canvas');
    return Boolean(canvas.getContext('webgl2') ?? canvas.getContext('webgl'));
  } catch {
    return false;
  }
}

/**
 * Loads MapLibre on demand (it is large and only the map screens need it) and registers the
 * pmtiles:// protocol once, so Protomaps basemaps load straight from the .pmtiles file.
 */
export function loadMapLibre(): Promise<MapLibreModule> {
  pending ??= Promise.all([import('maplibre-gl'), import('pmtiles')])
    .then(([ml, pm]) => {
      const lib = ((ml as unknown as { default?: MapLibreModule }).default ?? ml) as MapLibreModule;
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
