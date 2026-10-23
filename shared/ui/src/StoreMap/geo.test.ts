import { describe, expect, it } from 'vitest';
import { mapCenter } from '../test/fixtures';
import {
  circlePolygon,
  destination,
  pixelsPerMetre,
  radiusBounds,
  staticProjector,
  worldPixel,
  zoomToFitRadius,
} from './geo';

/** Haversine distance in metres, an independent check on destination(). */
function distance(a: { lng: number; lat: number }, b: { lng: number; lat: number }) {
  const r = (d: number) => (d * Math.PI) / 180;
  const h =
    Math.sin(r(b.lat - a.lat) / 2) ** 2 + Math.cos(r(a.lat)) * Math.cos(r(b.lat)) * Math.sin(r(b.lng - a.lng) / 2) ** 2;
  return 2 * 6_371_008.8 * Math.asin(Math.sqrt(h));
}

describe('geo', () => {
  it('destination lands the given distance away in the given direction', () => {
    const north = destination(mapCenter, 2000, 0);
    expect(distance(mapCenter, north)).toBeCloseTo(2000, 3);
    expect(north.lat).toBeGreaterThan(mapCenter.lat);
    expect(north.lng).toBeCloseTo(mapCenter.lng, 9);
    const east = destination(mapCenter, 5000, 90);
    expect(distance(mapCenter, east)).toBeCloseTo(5000, 3);
    expect(east.lng).toBeGreaterThan(mapCenter.lng);
  });

  it('destination wraps across the antimeridian', () => {
    const p = destination({ lng: 179.99, lat: 0 }, 5000, 90);
    expect(p.lng).toBeLessThan(-179);
  });

  it('circlePolygon is a closed ring at the radius', () => {
    const ring = circlePolygon(mapCenter, 2, 32).geometry.coordinates[0] ?? [];
    expect(ring).toHaveLength(33);
    expect(ring[0]).toEqual(ring[32]);
    for (const [lng, lat] of ring) {
      expect(distance(mapCenter, { lng: lng as number, lat: lat as number })).toBeCloseTo(2000, 2);
    }
  });

  it('radiusBounds surrounds the circle', () => {
    const [w, s, e, n] = radiusBounds(mapCenter, 1);
    expect(w).toBeLessThan(mapCenter.lng);
    expect(e).toBeGreaterThan(mapCenter.lng);
    expect(s).toBeLessThan(mapCenter.lat);
    expect(n).toBeGreaterThan(mapCenter.lat);
    expect(distance(mapCenter, { lng: mapCenter.lng, lat: n })).toBeCloseTo(1000, 3);
  });

  it('worldPixel clamps the poles', () => {
    expect(worldPixel({ lng: 0, lat: 90 }, 0)).toEqual(worldPixel({ lng: 0, lat: 85.0511 }, 0));
    expect(worldPixel({ lng: 0, lat: 0 }, 0)).toEqual([256, 256]);
  });

  it('zoomToFitRadius fits the ring inside the box with padding', () => {
    const zoom = zoomToFitRadius(mapCenter, 2, 312, 150, 12);
    const radiusPx = 2000 * pixelsPerMetre(mapCenter, zoom);
    expect(radiusPx).toBeCloseTo(150 / 2 - 12, 6);
    // A larger radius needs a lower zoom; a degenerate box still gives a finite zoom.
    expect(zoomToFitRadius(mapCenter, 20, 312, 150, 12)).toBeLessThan(zoom);
    expect(Number.isFinite(zoomToFitRadius(mapCenter, 2, 10, 10, 12))).toBe(true);
  });

  it('staticProjector puts the centre in the middle and reports the visible bounds', () => {
    const p = staticProjector(mapCenter, 13, 312, 150);
    const [x, y] = p.project(mapCenter);
    expect(x).toBeCloseTo(156, 6);
    expect(y).toBeCloseTo(75, 6);
    const [w, s, e, n] = p.bounds;
    expect(w).toBeLessThan(mapCenter.lng);
    expect(e).toBeGreaterThan(mapCenter.lng);
    expect(s).toBeLessThan(mapCenter.lat);
    expect(n).toBeGreaterThan(mapCenter.lat);
    const [nx, ny] = p.project({ lng: w, lat: n });
    expect(nx).toBeCloseTo(0, 4);
    expect(ny).toBeCloseTo(0, 4);
    expect(p.zoom).toBe(13);
  });
});
