import { color } from '@todayz/tokens';
import { describe, expect, it } from 'vitest';
import { mapCenter } from '../test/fixtures';
import { buildMapStyle, fixtureMapStyle, mapColors } from './style';

const tokenValues = new Set<string>(Object.values(color));

/** Every colour value in a style's paint properties. */
function paintColours(layers: readonly { paint?: unknown }[]): string[] {
  return layers.flatMap((l) =>
    Object.entries((l.paint ?? {}) as Record<string, unknown>)
      .filter(([k, v]) => k.endsWith('-color') && typeof v === 'string')
      .map(([, v]) => v as string),
  );
}

describe('map style', () => {
  it('takes every colour from the tokens', () => {
    for (const v of Object.values(mapColors)) expect(tokenValues).toContain(v);
    expect(mapColors.land).toBe(color['map-land']);
    for (const c of paintColours(buildMapStyle({ tilesUrl: 'x', glyphsUrl: 'g/{fontstack}/{range}.pbf' }).layers)) {
      expect(tokenValues).toContain(c);
    }
    for (const c of paintColours(fixtureMapStyle(mapCenter).layers)) expect(tokenValues).toContain(c);
  });

  it('builds a Protomaps style over pmtiles without labels when there are no glyphs', () => {
    const style = buildMapStyle({ tilesUrl: 'https://tiles.todayz.localhost/basemap.pmtiles' });
    expect(style.version).toBe(8);
    expect(style.sources['protomaps']).toMatchObject({
      type: 'vector',
      url: 'pmtiles://https://tiles.todayz.localhost/basemap.pmtiles',
    });
    expect(style.glyphs).toBeUndefined();
    expect(style.layers.map((l) => l.id)).toEqual([
      'background',
      'earth',
      'landuse-park',
      'water',
      'roads-casing',
      'roads',
    ]);
  });

  it('adds road labels with the given glyphs and font stack', () => {
    const style = buildMapStyle({ tilesUrl: 't', glyphsUrl: 'g/{fontstack}/{range}.pbf', fontStack: ['Figtree'] });
    expect(style.glyphs).toBe('g/{fontstack}/{range}.pbf');
    const labels = style.layers.find((l) => l.id === 'road-labels');
    expect(labels).toMatchObject({ type: 'symbol', layout: { 'text-font': ['Figtree'] } });
  });

  it('the fixture style is self-contained GeoJSON (no network)', () => {
    const style = fixtureMapStyle(mapCenter);
    expect(style.glyphs).toBeUndefined();
    for (const src of Object.values(style.sources)) expect(src.type).toBe('geojson');
    expect(JSON.stringify(style)).not.toMatch(/https?:|pmtiles:/);
    const roads = style.sources['roads'] as { data: { features: unknown[] } };
    expect(roads.data.features.length).toBeGreaterThan(0);
  });
});
