import type { Feature, Polygon } from 'geojson';

export interface LngLat {
  lng: number;
  lat: number;
}

const EARTH_RADIUS_M = 6_371_008.8;
const EARTH_CIRCUMFERENCE_M = 40_075_016.686;
/** MapLibre renders 512px tiles, so world size at zoom z is 512 * 2^z. */
export const TILE_SIZE = 512;

const toRad = (d: number) => (d * Math.PI) / 180;
const toDeg = (r: number) => (r * 180) / Math.PI;

/** The point `distanceM` metres from `from` along `bearingDeg` on a sphere. */
export function destination(from: LngLat, distanceM: number, bearingDeg: number): LngLat {
  const d = distanceM / EARTH_RADIUS_M;
  const b = toRad(bearingDeg);
  const lat1 = toRad(from.lat);
  const lng1 = toRad(from.lng);
  const lat2 = Math.asin(Math.sin(lat1) * Math.cos(d) + Math.cos(lat1) * Math.sin(d) * Math.cos(b));
  const lng2 =
    lng1 + Math.atan2(Math.sin(b) * Math.sin(d) * Math.cos(lat1), Math.cos(d) - Math.sin(lat1) * Math.sin(lat2));
  return { lng: ((toDeg(lng2) + 540) % 360) - 180, lat: toDeg(lat2) };
}

/** A geodesic circle as a closed GeoJSON polygon (the search radius ring). */
export function circlePolygon(center: LngLat, radiusKm: number, steps = 64): Feature<Polygon> {
  const ring: [number, number][] = [];
  for (let i = 0; i < steps; i++) {
    const p = destination(center, radiusKm * 1000, (i * 360) / steps);
    ring.push([p.lng, p.lat]);
  }
  ring.push(ring[0] as [number, number]);
  return { type: 'Feature', properties: {}, geometry: { type: 'Polygon', coordinates: [ring] } };
}

/** [west, south, east, north] of the circle. */
export function radiusBounds(center: LngLat, radiusKm: number): [number, number, number, number] {
  const n = destination(center, radiusKm * 1000, 0);
  const e = destination(center, radiusKm * 1000, 90);
  const s = destination(center, radiusKm * 1000, 180);
  const w = destination(center, radiusKm * 1000, 270);
  return [w.lng, s.lat, e.lng, n.lat];
}

/** Web Mercator world pixel of a point at `zoom` (512px tiles, as MapLibre). */
export function worldPixel(p: LngLat, zoom: number): [number, number] {
  const size = TILE_SIZE * 2 ** zoom;
  const x = ((p.lng + 180) / 360) * size;
  const sin = Math.sin(toRad(Math.max(-85.0511, Math.min(85.0511, p.lat))));
  const y = (0.5 - Math.log((1 + sin) / (1 - sin)) / (4 * Math.PI)) * size;
  return [x, y];
}

/**
 * The zoom at which a circle of `radiusKm` around `center` fits a `width` × `height` box with
 * `padding` pixels to spare on every side.
 */
export function zoomToFitRadius(
  center: LngLat,
  radiusKm: number,
  width: number,
  height: number,
  padding: number,
): number {
  const half = Math.max(1, Math.min(width, height) / 2 - padding);
  const metresPerPixel = (radiusKm * 1000) / half;
  const worldSize = (EARTH_CIRCUMFERENCE_M * Math.cos(toRad(center.lat))) / metresPerPixel;
  return Math.log2(worldSize / TILE_SIZE);
}

/** Pixels per metre at `center` and `zoom`. */
export function pixelsPerMetre(center: LngLat, zoom: number): number {
  return (TILE_SIZE * 2 ** zoom) / (EARTH_CIRCUMFERENCE_M * Math.cos(toRad(center.lat)));
}

export interface Projector {
  /** Screen pixel of a point inside the map box. */
  project: (p: LngLat) => [number, number];
  zoom: number;
  /** [west, south, east, north] of the visible box. */
  bounds: [number, number, number, number];
}

/** A static projection centred on `center` (used when WebGL is unavailable). */
export function staticProjector(center: LngLat, zoom: number, width: number, height: number): Projector {
  const [cx, cy] = worldPixel(center, zoom);
  const size = TILE_SIZE * 2 ** zoom;
  const unproject = (x: number, y: number): LngLat => {
    const wx = cx + x - width / 2;
    const wy = cy + y - height / 2;
    const lng = (wx / size) * 360 - 180;
    const n = Math.PI - (2 * Math.PI * wy) / size;
    return { lng, lat: toDeg(Math.atan(Math.sinh(n))) };
  };
  const nw = unproject(0, 0);
  const se = unproject(width, height);
  return {
    zoom,
    bounds: [nw.lng, se.lat, se.lng, nw.lat],
    project: (p) => {
      const [x, y] = worldPixel(p, zoom);
      return [x - cx + width / 2, y - cy + height / 2];
    },
  };
}
