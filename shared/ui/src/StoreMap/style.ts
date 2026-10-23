import { color } from '@todayz/tokens';
import type { FeatureCollection } from 'geojson';
import type { StyleSpecification } from 'maplibre-gl';
import { destination, type LngLat } from './geo';

/** Map colours, all from the map-* and brand tokens (no literals). */
export const mapColors = {
  land: color['map-land'],
  park: color['map-park'],
  water: color['map-water'],
  road: color['map-road'],
  roadEdge: color['map-road-edge'],
  ring: color.brand,
  ringLine: color['brand-mark'],
  label: color['ink-2'],
  halo: color.surface,
} as const;

export interface MapStyleOptions {
  /** URL of the Protomaps basemap .pmtiles file (served from tiles.todayz.in / tiles.todayz.localhost). */
  tilesUrl: string;
  /** Glyph URL template ("…/{fontstack}/{range}.pbf"). Without it the style has no text labels. */
  glyphsUrl?: string | undefined;
  /** Font stack for road labels, as named in the glyph set. */
  fontStack?: readonly string[] | undefined;
}

const PARK_KINDS = [
  'park',
  'garden',
  'golf_course',
  'nature_reserve',
  'forest',
  'wood',
  'grass',
  'playground',
  'pitch',
  'cemetery',
];

/**
 * A light basemap for the Protomaps v4 schema (earth, landuse, water, roads) painted with the
 * map-* tokens. The pmtiles:// protocol is registered by the loader.
 */
export function buildMapStyle({
  tilesUrl,
  glyphsUrl,
  fontStack = ['Noto Sans Regular'],
}: MapStyleOptions): StyleSpecification {
  const roadWidth = (major: number, minor: number) =>
    [
      'interpolate',
      ['exponential', 1.6],
      ['zoom'],
      12,
      ['match', ['get', 'kind'], ['highway', 'major_road'], major * 0.4, minor * 0.3],
      18,
      ['match', ['get', 'kind'], ['highway', 'major_road'], major * 3, minor * 2.4],
    ] as unknown as number;
  const style: StyleSpecification = {
    version: 8,
    sources: {
      protomaps: {
        type: 'vector',
        url: `pmtiles://${tilesUrl}`,
        attribution: '© OpenStreetMap',
      },
    },
    layers: [
      { id: 'background', type: 'background', paint: { 'background-color': mapColors.land } },
      {
        id: 'earth',
        type: 'fill',
        source: 'protomaps',
        'source-layer': 'earth',
        paint: { 'fill-color': mapColors.land },
      },
      {
        id: 'landuse-park',
        type: 'fill',
        source: 'protomaps',
        'source-layer': 'landuse',
        filter: ['in', ['get', 'kind'], ['literal', PARK_KINDS]],
        paint: { 'fill-color': mapColors.park },
      },
      {
        id: 'water',
        type: 'fill',
        source: 'protomaps',
        'source-layer': 'water',
        paint: { 'fill-color': mapColors.water },
      },
      {
        id: 'roads-casing',
        type: 'line',
        source: 'protomaps',
        'source-layer': 'roads',
        filter: ['in', ['get', 'kind'], ['literal', ['highway', 'major_road', 'medium_road', 'minor_road']]],
        layout: { 'line-cap': 'round', 'line-join': 'round' },
        paint: { 'line-color': mapColors.roadEdge, 'line-width': roadWidth(7, 4) },
      },
      {
        id: 'roads',
        type: 'line',
        source: 'protomaps',
        'source-layer': 'roads',
        filter: ['in', ['get', 'kind'], ['literal', ['highway', 'major_road', 'medium_road', 'minor_road']]],
        layout: { 'line-cap': 'round', 'line-join': 'round' },
        paint: { 'line-color': mapColors.road, 'line-width': roadWidth(5.5, 3) },
      },
    ],
  };
  if (glyphsUrl) {
    style.glyphs = glyphsUrl;
    style.layers.push({
      id: 'road-labels',
      type: 'symbol',
      source: 'protomaps',
      'source-layer': 'roads',
      minzoom: 14,
      layout: {
        'symbol-placement': 'line',
        'text-field': ['get', 'name'],
        'text-font': [...fontStack],
        'text-size': 11,
      },
      paint: { 'text-color': mapColors.label, 'text-halo-color': mapColors.halo, 'text-halo-width': 1.5 },
    });
  }
  return style;
}

/**
 * A self-contained style with synthetic parks, water and roads around `center`, as inline GeoJSON.
 * Needs no network, tiles or glyphs, so tests and Storybook render the map offline.
 */
export function fixtureMapStyle(center: LngLat): StyleSpecification {
  const at = (northKm: number, eastKm: number): [number, number] => {
    const n = destination(center, Math.abs(northKm) * 1000, northKm >= 0 ? 0 : 180);
    const p = destination(n, Math.abs(eastKm) * 1000, eastKm >= 0 ? 90 : 270);
    return [p.lng, p.lat];
  };
  const poly = (pts: [number, number][]) => ({
    type: 'Feature' as const,
    properties: {},
    geometry: { type: 'Polygon' as const, coordinates: [[...pts, pts[0] as [number, number]]] },
  });
  const line = (kind: string, pts: [number, number][]) => ({
    type: 'Feature' as const,
    properties: { kind },
    geometry: { type: 'LineString' as const, coordinates: pts },
  });
  const parks: FeatureCollection = {
    type: 'FeatureCollection',
    features: [
      poly([at(-1.2, -3.5), at(-0.7, -2.4), at(-1.4, -1.6), at(-2.5, -1.8), at(-2.5, -3.5)]),
      poly([at(2.5, 1.9), at(1.9, 2.3), at(1.6, 3.5), at(2.5, 3.5)]),
    ],
  };
  const water: FeatureCollection = {
    type: 'FeatureCollection',
    features: [poly([at(-1.0, 2.4), at(-1.2, 3.1), at(-1.7, 3.3), at(-2.2, 3.0), at(-2.0, 2.3), at(-1.5, 2.1)])],
  };
  const roads: FeatureCollection = {
    type: 'FeatureCollection',
    features: [
      line('major_road', [at(0.8, -4), at(-0.6, 4)]),
      line('major_road', [at(2.6, -1.0), at(-2.6, 0.2)]),
      line('minor_road', [at(-1.6, -4), at(-1.1, -1), at(-1.9, 4)]),
      line('minor_road', [at(2.6, 1.6), at(-2.6, 1.1)]),
      line('minor_road', [at(2.6, -3.2), at(-2.6, -2.7)]),
    ],
  };
  const roadLayer = (id: string, colour: string, major: number, minor: number) => ({
    id,
    type: 'line' as const,
    source: 'roads',
    layout: { 'line-cap': 'round' as const, 'line-join': 'round' as const },
    paint: {
      'line-color': colour,
      'line-width': ['match', ['get', 'kind'], 'major_road', major, minor] as unknown as number,
    },
  });
  return {
    version: 8,
    sources: {
      parks: { type: 'geojson', data: parks },
      water: { type: 'geojson', data: water },
      roads: { type: 'geojson', data: roads },
    },
    layers: [
      { id: 'background', type: 'background', paint: { 'background-color': mapColors.land } },
      { id: 'parks', type: 'fill', source: 'parks', paint: { 'fill-color': mapColors.park } },
      { id: 'water', type: 'fill', source: 'water', paint: { 'fill-color': mapColors.water } },
      roadLayer('roads-casing', mapColors.roadEdge, 9, 6),
      roadLayer('roads', mapColors.road, 7, 4),
    ],
  };
}
