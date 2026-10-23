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
  beforeEach(() => {
    // The answer is cached per module, so each test probes a fresh copy.
    vi.resetModules();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('is true when a WebGL context can be created, and releases the probe context', async () => {
    const { hasWebGL } = await import('./loader');
    const loseContext = vi.fn();
    const ctx = { getExtension: (name: string) => (name === 'WEBGL_lose_context' ? { loseContext } : null) };
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(ctx as unknown as RenderingContext);
    expect(hasWebGL()).toBe(true);
    expect(loseContext).toHaveBeenCalledTimes(1);
  });

  it('probes once: later calls create no canvas or context', async () => {
    const { hasWebGL } = await import('./loader');
    const getContext = vi
      .spyOn(HTMLCanvasElement.prototype, 'getContext')
      .mockReturnValue({ getExtension: () => null } as unknown as RenderingContext);
    const create = vi.spyOn(document, 'createElement');
    expect(hasWebGL()).toBe(true);
    expect(hasWebGL()).toBe(true);
    expect(hasWebGL()).toBe(true);
    expect(create.mock.calls.filter(([tag]) => tag === 'canvas')).toHaveLength(1);
    expect(getContext).toHaveBeenCalledTimes(1);
  });

  it('is false without a context', async () => {
    const { hasWebGL } = await import('./loader');
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null);
    expect(hasWebGL()).toBe(false);
  });

  it('is false when creating one throws', async () => {
    const { hasWebGL } = await import('./loader');
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation(() => {
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
