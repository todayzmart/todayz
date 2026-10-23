import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const h = vi.hoisted(() => ({
  addProtocol: vi.fn(),
  setWorkerUrl: vi.fn(),
  fail: false,
  protocols: 0,
}));

vi.mock('maplibre-gl', () => ({
  default: { addProtocol: h.addProtocol, setWorkerUrl: h.setWorkerUrl, Map: vi.fn() },
}));
vi.mock('maplibre-gl/dist/maplibre-gl-worker.mjs?url', () => ({ default: '/assets/maplibre-gl-worker.mjs' }));
vi.mock('pmtiles', () => ({
  Protocol: class {
    constructor() {
      if (h.fail) throw new Error('pmtiles failed to load');
      h.protocols += 1;
    }
    tile = () => undefined;
  },
}));

describe('hasWebGL', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('is true when a WebGL context can be created', async () => {
    const { hasWebGL } = await import('./loader');
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({} as RenderingContext);
    expect(hasWebGL()).toBe(true);
  });

  it('is false without a context or when creating one throws', async () => {
    const { hasWebGL } = await import('./loader');
    const spy = vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null);
    expect(hasWebGL()).toBe(false);
    spy.mockImplementation(() => {
      throw new Error('blocked');
    });
    expect(hasWebGL()).toBe(false);
  });
});

describe('loadMapLibre', () => {
  beforeEach(() => {
    vi.resetModules();
    h.addProtocol.mockReset();
    h.setWorkerUrl.mockReset();
    h.protocols = 0;
    h.fail = false;
  });

  it('loads once, points MapLibre at the bundled worker and registers the pmtiles protocol once', async () => {
    const { loadMapLibre } = await import('./loader');
    const [a, b] = await Promise.all([loadMapLibre(), loadMapLibre()]);
    expect(a).toBe(b);
    expect(await loadMapLibre()).toBe(a);
    expect(h.addProtocol).toHaveBeenCalledTimes(1);
    expect(h.addProtocol).toHaveBeenCalledWith('pmtiles', expect.any(Function));
    expect(h.protocols).toBe(1);
    expect(h.setWorkerUrl).toHaveBeenCalledWith('/assets/maplibre-gl-worker.mjs');
  });

  it('rejects when loading fails and tries again next time', async () => {
    h.fail = true;
    const { loadMapLibre } = await import('./loader');
    await expect(loadMapLibre()).rejects.toThrow();
    h.fail = false;
    await expect(loadMapLibre()).resolves.toBeDefined();
    expect(h.addProtocol).toHaveBeenCalledTimes(1);
  });
});
