import { act, fireEvent, render, screen } from '@testing-library/react';
import type { StyleSpecification } from 'maplibre-gl';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { makeShops, mapCenter, radiusOptions } from '../test/fixtures';
import { staticProjector, type LngLat } from './geo';
import { StoreMap, type StoreMapProps } from './StoreMap';

type Handler = () => void;

/** A stand-in for maplibre-gl's Map that projects with the static projector. */
class FakeMap {
  static instances: FakeMap[] = [];
  handlers = new Map<string, Handler[]>();
  sources = new Map<string, { setData: ReturnType<typeof vi.fn> }>();
  layers: string[] = [];
  zoom = 13;
  center: LngLat = mapCenter;
  touchZoomRotate = { disableRotation: vi.fn() };
  fitBounds = vi.fn();
  easeTo = vi.fn();
  setStyle = vi.fn();
  remove = vi.fn();
  constructor(public options: Record<string, unknown>) {
    FakeMap.instances.push(this);
  }
  private get projector() {
    return staticProjector(this.center, this.zoom, 312, 150);
  }
  on(event: string, fn: Handler) {
    this.handlers.set(event, [...(this.handlers.get(event) ?? []), fn]);
    return this;
  }
  emit(event: string) {
    for (const fn of this.handlers.get(event) ?? []) fn();
  }
  addSource(id: string) {
    this.sources.set(id, { setData: vi.fn() });
  }
  addLayer(layer: { id: string }) {
    this.layers.push(layer.id);
  }
  getSource(id: string) {
    return this.sources.get(id);
  }
  getZoom() {
    return this.zoom;
  }
  getBounds() {
    const [w, s, e, n] = this.projector.bounds;
    return { getWest: () => w, getSouth: () => s, getEast: () => e, getNorth: () => n };
  }
  project([lng, lat]: [number, number]) {
    const [x, y] = this.projector.project({ lng, lat });
    return { x, y };
  }
}

const loader = vi.hoisted(() => ({
  webgl: false,
  load: null as null | (() => Promise<unknown>),
}));

vi.mock('./loader', () => ({
  hasWebGL: () => loader.webgl,
  loadMapLibre: () => (loader.load ? loader.load() : Promise.reject(new Error('no WebGL'))),
}));

const shops = makeShops(43);

function props(over: Partial<StoreMapProps> = {}): StoreMapProps {
  return {
    label: 'Shops on the map',
    mapLabel: 'Map: 43 shops within 2 km, 38 open now',
    center: mapCenter,
    shops,
    radiusKm: 2,
    radiusOptions,
    radiusLabel: 'Search radius',
    onRadiusChange: vi.fn(),
    onSelectShop: vi.fn(),
    pinLabel: (s) => `${s.name}, ${s.open ? 'open' : 'closed'}`,
    clusterLabel: (n) => `${n} shops, zoom in`,
    youLabel: 'You are here',
    legend: { open: 'Open', closed: 'Closed', you: 'You' },
    ...over,
  };
}

const pins = () => screen.queryAllByRole('button', { name: /, (open|closed)$/ });
const clusters = () => screen.queryAllByRole('button', { name: /shops, zoom in$/ });
const settle = () => act(async () => Promise.resolve());

describe('StoreMap (static fallback)', () => {
  beforeEach(() => {
    loader.webgl = false;
    loader.load = null;
  });

  it('draws pins, clusters, the customer and the ring without WebGL', () => {
    const { container } = render(<StoreMap {...props()} liveLabel="Live near you" />);
    const map = screen.getByRole('group', { name: 'Map: 43 shops within 2 km, 38 open now' });
    expect(map).toHaveAttribute('data-mode', 'static');
    expect(screen.getByRole('region', { name: 'Shops on the map' })).toBeInTheDocument();
    expect(screen.getByRole('img', { name: 'You are here' })).toBeInTheDocument();
    expect(container.querySelector('circle.tz-ui-map-ring')).not.toBeNull();
    expect(pins().length + clusters().length).toBeGreaterThan(0);
    expect(clusters().length).toBeGreaterThan(0);
    expect(screen.getByText('Live near you')).toBeInTheDocument();
    expect(screen.queryByText('© OpenStreetMap')).toBeNull();
  });

  it('marks open pins and reports the tapped shop', () => {
    const p = props();
    render(<StoreMap {...p} />);
    const pin = pins()[0];
    if (!pin) throw new Error('no pin rendered');
    fireEvent.click(pin);
    expect(p.onSelectShop).toHaveBeenCalledTimes(1);
    const name = pin.getAttribute('aria-label') ?? '';
    expect(pin.hasAttribute('data-open')).toBe(name.endsWith(', open'));
  });

  it('zooms into a cluster, and a new radius resets the view', () => {
    const p = props();
    const { rerender } = render(<StoreMap {...p} />);
    const before = pins().length;
    const biggest = clusters().sort(
      (a, b) => parseInt(b.textContent ?? '0', 10) - parseInt(a.textContent ?? '0', 10),
    )[0];
    if (!biggest) throw new Error('no cluster rendered');
    fireEvent.click(biggest);
    expect(pins().length).toBeGreaterThan(before);
    rerender(<StoreMap {...p} radiusKm={5} />);
    rerender(<StoreMap {...p} radiusKm={2} />);
    expect(pins()).toHaveLength(before);
  });

  it('shows stats, the expand button and the radius chips', () => {
    const p = props({
      stats: [{ key: 'shops', value: '43', label: 'Shops nearby' }],
      expandLabel: 'Open full map',
      onExpand: vi.fn(),
    });
    render(<StoreMap {...p} />);
    expect(screen.getByText('Shops nearby')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Open full map' }));
    expect(p.onExpand).toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: '5 km' }));
    expect(p.onRadiusChange).toHaveBeenCalledWith(5);
  });

  it('skips pins for unknown shops and an empty list', () => {
    render(<StoreMap {...props({ shops: [] })} />);
    expect(pins()).toHaveLength(0);
    expect(clusters()).toHaveLength(0);
    expect(screen.queryByRole('button', { name: 'Open full map' })).toBeNull();
  });

  it('uses the static renderer when asked even with WebGL', () => {
    loader.webgl = true;
    loader.load = vi.fn(() => Promise.resolve({ Map: FakeMap }));
    render(<StoreMap {...props({ renderer: 'static' })} />);
    expect(screen.getByRole('group', { name: /^Map:/ })).toHaveAttribute('data-mode', 'static');
    expect(loader.load).not.toHaveBeenCalled();
  });
});

describe('StoreMap (MapLibre)', () => {
  beforeEach(() => {
    FakeMap.instances = [];
    loader.webgl = true;
    loader.load = () => Promise.resolve({ Map: FakeMap });
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: true } as MediaQueryList);
  });

  async function mount(over: Partial<StoreMapProps> = {}) {
    const p = props(over);
    const view = render(<StoreMap {...p} />);
    await settle();
    const map = FakeMap.instances[0];
    if (!map) throw new Error('map not created');
    return { p, view, map };
  }

  it('loads the map, adds the ring once loaded, then shows pins and the credit', async () => {
    const { map } = await mount({ interactive: false });
    const box = screen.getByRole('group', { name: /^Map:/ });
    expect(box).toHaveAttribute('data-mode', 'loading');
    expect(map.options).toMatchObject({ interactive: false, attributionControl: false, dragRotate: false });
    expect((map.options['style'] as StyleSpecification).sources).toHaveProperty('roads');
    expect(map.touchZoomRotate.disableRotation).toHaveBeenCalled();
    act(() => map.emit('load'));
    expect(box).toHaveAttribute('data-mode', 'webgl');
    expect(map.layers).toEqual(['radius-fill', 'radius-line']);
    expect(pins().length + clusters().length).toBeGreaterThan(0);
    expect(screen.getByText('© OpenStreetMap')).toBeInTheDocument();
    act(() => map.emit('move'));
    act(() => map.emit('resize'));
  });

  it('redraws and fits the ring when the radius changes, eases into clusters, and removes the map', async () => {
    const { p, view, map } = await mount();
    act(() => map.emit('load'));
    map.fitBounds.mockClear();
    view.rerender(<StoreMap {...p} radiusKm={5} />);
    expect(map.sources.get('radius')?.setData).toHaveBeenCalled();
    expect(map.fitBounds).toHaveBeenCalledWith(expect.any(Array), { padding: 12, duration: 0 });
    const cluster = clusters()[0];
    if (!cluster) throw new Error('no cluster rendered');
    fireEvent.click(cluster);
    expect(map.easeTo).toHaveBeenCalledWith(expect.objectContaining({ duration: 0 }));
    view.unmount();
    expect(map.remove).toHaveBeenCalled();
  });

  it('swaps in a new style and keeps the ring layers', async () => {
    const { p, view, map } = await mount();
    act(() => map.emit('load'));
    const next: StyleSpecification = { version: 8, sources: {}, layers: [] };
    view.rerender(<StoreMap {...p} mapStyle={next} />);
    expect(map.setStyle).toHaveBeenCalledTimes(1);
    const [, opts] = map.setStyle.mock.calls[0] as [
      StyleSpecification,
      { transformStyle: (a: unknown, b: StyleSpecification) => StyleSpecification },
    ];
    const out = opts.transformStyle(undefined, next);
    expect(Object.keys(out.sources)).toEqual(['radius']);
    expect(out.layers.map((l) => l.id)).toEqual(['radius-fill', 'radius-line']);
  });

  it('falls back to the static map when MapLibre fails to load', async () => {
    loader.load = () => Promise.reject(new Error('offline'));
    render(<StoreMap {...props()} />);
    await settle();
    expect(screen.getByRole('group', { name: /^Map:/ })).toHaveAttribute('data-mode', 'static');
  });

  it('ignores a load that finishes after unmount', async () => {
    let resolve: (v: unknown) => void = () => undefined;
    loader.load = () => new Promise((r) => (resolve = r));
    const view = render(<StoreMap {...props()} />);
    view.unmount();
    resolve({ Map: FakeMap });
    await settle();
    expect(FakeMap.instances).toHaveLength(0);
  });
});
