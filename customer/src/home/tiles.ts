// Which basemap file covers a point. Local tiles are one .pmtiles archive per city (infra/local/tiles.sh);
// outside every city the nearest one is used, so the map still draws (the pins and ring do not depend on tiles).
// TODO(lead): production tile layout (one India file or per-city files) is not decided; this assumes per-city names.
import { TILES_URL } from '../app/env';

interface City {
  name: string;
  /** min lng, min lat, max lng, max lat: the same boxes `infra/local/tiles.sh` extracts. */
  bbox: readonly [number, number, number, number];
}

const CITIES: readonly City[] = [
  { name: 'bengaluru', bbox: [77.3, 12.7, 77.95, 13.25] },
  { name: 'hyderabad', bbox: [78.15, 17.2, 78.75, 17.65] },
  { name: 'chennai', bbox: [79.95, 12.75, 80.35, 13.3] },
];

function distanceToBox(lat: number, lng: number, [w, s, e, n]: City['bbox']): number {
  const dx = Math.max(w - lng, 0, lng - e);
  const dy = Math.max(s - lat, 0, lat - n);
  return Math.hypot(dx, dy);
}

export function cityFor(lat: number, lng: number): string {
  let best = CITIES[0] as City;
  let bestD = Infinity;
  for (const c of CITIES) {
    const d = distanceToBox(lat, lng, c.bbox);
    if (d < bestD) {
      best = c;
      bestD = d;
    }
  }
  return best.name;
}

/** The .pmtiles URL for a point, or undefined when no tile host is configured (the map then uses its offline style). */
export function tilesUrlFor(lat: number, lng: number): string | undefined {
  if (TILES_URL === '') return undefined;
  return `${TILES_URL}/${cityFor(lat, lng)}.pmtiles`;
}
