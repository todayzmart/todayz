import 'maplibre-gl/dist/maplibre-gl.css';
import { Maximize2 } from 'lucide-react';
import type { GeoJSONSource, Map as MapLibreMap, StyleSpecification } from 'maplibre-gl';
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from 'react';
import Supercluster, { type ClusterProperties, type PointFeature } from 'supercluster';
import { RadiusChips } from '../Chip/Chip';
import { cx } from '../internal/cx';
import { Icon } from '../internal/Icon';
import {
  circlePolygon,
  pixelsPerMetre,
  radiusBounds,
  staticProjector,
  zoomToFitRadius,
  type LngLat,
  type Projector,
} from './geo';
import { hasWebGL, loadMapLibre } from './loader';
import { fixtureMapStyle, mapColors } from './style';

export interface MapShop {
  id: string;
  name: string;
  lng: number;
  lat: number;
  open: boolean;
}

export interface StoreMapProps {
  /** Accessible name of the card, e.g. "Shops on the map". */
  label: string;
  /** Summary read for the map area, e.g. "Map: 43 shops within 2 km, 38 open now". */
  mapLabel: string;
  /** The customer's location; the radius ring is centred here. */
  center: LngLat;
  shops: readonly MapShop[];
  /** Current radius in km, one of `radiusOptions`. */
  radiusKm: number;
  /** From platform settings (01-product-rules), each with its label ("2 km"). */
  radiusOptions: readonly { km: number; label: string }[];
  /** Name of the radius chip group, e.g. "Search radius". */
  radiusLabel: string;
  onRadiusChange: (km: number) => void;
  /** A pin was tapped; the app opens the shop sheet. */
  onSelectShop: (id: string) => void;
  /** Name of a pin, e.g. (s) => `${s.name}, open`. */
  pinLabel: (shop: MapShop) => string;
  /** Name of a cluster, e.g. (n) => `${n} shops, zoom in`. */
  clusterLabel: (count: number) => string;
  /** Name of the customer's dot, e.g. "You are here". */
  youLabel: string;
  legend: { open: string; closed: string; you: string };
  /** Badge top left, e.g. "Live near you". */
  liveLabel?: string | undefined;
  stats?: readonly { key: string; value: ReactNode; label: ReactNode }[] | undefined;
  /** "Open full map" button; shown when onExpand is set. */
  expandLabel?: string | undefined;
  onExpand?: (() => void) | undefined;
  /**
   * MapLibre style. Production passes `buildMapStyle({ tilesUrl })`; the default is the offline
   * fixture around `center`.
   */
  mapStyle?: StyleSpecification | undefined;
  /** Map data credit, shown in WebGL mode (the static fallback draws no map data). */
  attribution?: string | undefined;
  /**
   * Map height in px. Without it the map keeps the design card's 312 × 150 proportions at
   * whatever width the card has.
   */
  height?: number | undefined;
  /** auto: MapLibre when WebGL works, otherwise the static fallback. */
  renderer?: 'auto' | 'static' | undefined;
  /** Pan and zoom by touch and mouse. Off for the small home card that opens the full map. */
  interactive?: boolean | undefined;
  className?: string | undefined;
}

type Mode = 'loading' | 'webgl' | 'static';
type ShopPoint = PointFeature<{ id: string }>;

const PADDING = 12;
/** The design card's map proportions (its sketch is drawn in a 312 × 150 box). */
const DESIGN_WIDTH = 312;
const DESIGN_HEIGHT = 150;
const FIT_MS = 400;

function prefersReducedMotion(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
  );
}

/**
 * Shops near the customer on a light map (MapLibre + a Protomaps basemap painted with the map-*
 * tokens): open and closed pins, clusters, the customer's dot and the dashed radius ring, with
 * stats and radius chips below. Pins are real buttons in the DOM, so they are focusable and
 * named. Without WebGL it falls back to a static projection with the same pins and ring.
 */
export function StoreMap({
  label,
  mapLabel,
  center,
  shops,
  radiusKm,
  radiusOptions,
  radiusLabel,
  onRadiusChange,
  onSelectShop,
  pinLabel,
  clusterLabel,
  youLabel,
  legend,
  liveLabel,
  stats,
  expandLabel,
  onExpand,
  mapStyle,
  attribution = '© OpenStreetMap',
  height,
  renderer = 'auto',
  interactive = true,
  className,
}: StoreMapProps) {
  const boxRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLDivElement>(null);
  const mapRef = useRef<MapLibreMap | null>(null);
  const [webgl] = useState(hasWebGL);
  const [gl, setGl] = useState<'loading' | 'ready' | 'failed'>('loading');
  const mode: Mode =
    renderer === 'static' || !webgl || gl === 'failed' ? 'static' : gl === 'ready' ? 'webgl' : 'loading';
  const [size, setSize] = useState({ width: 0, height: 0 });
  const [glProjector, setGlProjector] = useState<Projector | null>(null);
  /** Static mode zooms into a cluster by moving its own view; a new radius or location resets it. */
  const viewKey = `${radiusKm}|${center.lng}|${center.lat}`;
  const [staticView, setStaticView] = useState<{ center: LngLat; zoom: number } | null>(null);
  const [viewFor, setViewFor] = useState(viewKey);
  if (viewFor !== viewKey) {
    // Reset during render (not in an effect) so the old zoom never paints with the new radius.
    setViewFor(viewKey);
    setStaticView(null);
  }

  // Latest values for MapLibre callbacks without re-creating the map.
  const latest = useRef({ center, radiusKm, mapStyle, mapLabel });
  useLayoutEffect(() => {
    latest.current = { center, radiusKm, mapStyle, mapLabel };
  });

  const index = useMemo(() => {
    const sc = new Supercluster<{ id: string }>({ radius: 40, maxZoom: 18 });
    const points: ShopPoint[] = shops.map((s) => ({
      type: 'Feature',
      properties: { id: s.id },
      geometry: { type: 'Point', coordinates: [s.lng, s.lat] },
    }));
    sc.load(points);
    return sc;
  }, [shops]);
  const byId = useMemo(() => new Map(shops.map((s) => [s.id, s])), [shops]);

  // Track the box width (static projection and marker culling).
  useLayoutEffect(() => {
    const el = boxRef.current;
    if (!el) return undefined;
    const measure = () => setSize({ width: el.clientWidth, height: el.clientHeight });
    measure();
    if (typeof ResizeObserver === 'undefined') return undefined;
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const syncProjector = useCallback(() => {
    const map = mapRef.current;
    if (!map) return;
    const b = map.getBounds();
    setGlProjector({
      zoom: map.getZoom(),
      bounds: [b.getWest(), b.getSouth(), b.getEast(), b.getNorth()],
      project: (p) => {
        const pt = map.project([p.lng, p.lat]);
        return [pt.x, pt.y];
      },
    });
  }, []);

  // Create the MapLibre map once.
  useEffect(() => {
    if (renderer === 'static' || !webgl) return undefined;
    let cancelled = false;
    let map: MapLibreMap | null = null;
    loadMapLibre()
      .then((ml) => {
        const container = canvasRef.current;
        if (cancelled || !container) return;
        const { center: c, radiusKm: r, mapStyle: s } = latest.current;
        map = new ml.Map({
          container,
          style: s ?? fixtureMapStyle(c),
          bounds: radiusBounds(c, r),
          fitBoundsOptions: { padding: PADDING },
          attributionControl: false,
          interactive,
          dragRotate: false,
          pitchWithRotate: false,
          touchPitch: false,
          fadeDuration: 0,
          // The interactive canvas is a focusable region; name it from props, not MapLibre's "Map".
          locale: { 'Map.Title': latest.current.mapLabel },
        });
        map.touchZoomRotate.disableRotation();
        mapRef.current = map;
        map.on('load', () => {
          const m = mapRef.current;
          if (!m) return;
          m.addSource('radius', {
            type: 'geojson',
            data: circlePolygon(latest.current.center, latest.current.radiusKm),
          });
          m.addLayer({
            id: 'radius-fill',
            type: 'fill',
            source: 'radius',
            paint: { 'fill-color': mapColors.ring, 'fill-opacity': 0.06 },
          });
          m.addLayer({
            id: 'radius-line',
            type: 'line',
            source: 'radius',
            paint: { 'line-color': mapColors.ringLine, 'line-width': 1.5, 'line-dasharray': [3, 3] },
          });
          syncProjector();
          setGl('ready');
        });
        map.on('move', syncProjector);
        map.on('resize', syncProjector);
      })
      .catch(() => {
        if (!cancelled) setGl('failed');
      });
    return () => {
      cancelled = true;
      map?.remove();
      mapRef.current = null;
      setGl('loading');
      setGlProjector(null);
    };
    // The map is created once; later prop changes are applied by the effects below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [renderer, webgl]);

  // Radius or location changed: redraw the ring and fit it within 500 ms.
  useEffect(() => {
    const map = mapRef.current;
    if (mode !== 'webgl' || !map) return;
    const src = map.getSource<GeoJSONSource>('radius');
    src?.setData(circlePolygon(center, radiusKm));
    map.fitBounds(radiusBounds(center, radiusKm), { padding: PADDING, duration: prefersReducedMotion() ? 0 : FIT_MS });
  }, [mode, radiusKm, center]);

  // A new style (e.g. tiles URL from config) replaces the basemap; the ring layers are re-added on styledata.
  useEffect(() => {
    const map = mapRef.current;
    if (mode !== 'webgl' || !map || !mapStyle) return;
    map.setStyle(mapStyle, {
      transformStyle: (_prev, next) => ({
        ...next,
        sources: {
          ...next.sources,
          radius: { type: 'geojson', data: circlePolygon(latest.current.center, latest.current.radiusKm) },
        },
        layers: [
          ...next.layers,
          {
            id: 'radius-fill',
            type: 'fill',
            source: 'radius',
            paint: { 'fill-color': mapColors.ring, 'fill-opacity': 0.06 },
          },
          {
            id: 'radius-line',
            type: 'line',
            source: 'radius',
            paint: { 'line-color': mapColors.ringLine, 'line-width': 1.5, 'line-dasharray': [3, 3] },
          },
        ],
      }),
    });
  }, [mode, mapStyle]);

  const boxWidth = size.width || DESIGN_WIDTH;
  // Measured once laid out; before that (and in tests without layout) the prop or the design ratio.
  const boxHeight = size.height || height || (boxWidth * DESIGN_HEIGHT) / DESIGN_WIDTH;
  const projector: Projector | null = useMemo(() => {
    if (mode === 'webgl') return glProjector;
    if (mode !== 'static') return null;
    const view = staticView ?? { center, zoom: zoomToFitRadius(center, radiusKm, boxWidth, boxHeight, PADDING) };
    return staticProjector(view.center, view.zoom, boxWidth, boxHeight);
  }, [mode, glProjector, staticView, center, radiusKm, boxWidth, boxHeight]);

  const clusters = useMemo(() => {
    if (!projector) return [];
    const [w, s, e, n] = projector.bounds;
    return index.getClusters([w, s, e, n], Math.floor(projector.zoom));
  }, [index, projector]);

  const openCluster = (clusterId: number, at: LngLat) => {
    const zoom = Math.min(index.getClusterExpansionZoom(clusterId), 18);
    const map = mapRef.current;
    if (mode === 'webgl' && map) {
      map.easeTo({ center: [at.lng, at.lat], zoom, duration: prefersReducedMotion() ? 0 : FIT_MS });
    } else {
      setStaticView({ center: at, zoom: zoom + 0.01 });
    }
  };

  const inBox = ([x, y]: [number, number]) => x >= -16 && y >= -16 && x <= boxWidth + 16 && y <= boxHeight + 16;
  const place = ([x, y]: [number, number]): CSSProperties => ({ transform: `translate(${x}px, ${y}px)` });

  const ring =
    mode === 'static' && projector ? (
      <svg className="tz-ui-map-static" width={boxWidth} height={boxHeight} aria-hidden="true" focusable="false">
        <rect width="100%" height="100%" className="tz-ui-map-land" />
        <circle
          cx={projector.project(center)[0]}
          cy={projector.project(center)[1]}
          r={radiusKm * 1000 * pixelsPerMetre(center, projector.zoom)}
          className="tz-ui-map-ring"
        />
      </svg>
    ) : null;

  return (
    <section className={cx('tz-map-card', className)} aria-label={label}>
      <div
        ref={boxRef}
        className="tz-map tz-ui-map"
        style={height === undefined ? undefined : { height }}
        role="group"
        aria-label={mapLabel}
        data-mode={mode}
      >
        {height === undefined && <div className="tz-ui-map-sizer" />}
        {/* Interactive, MapLibre's canvas takes focus for keyboard panning, so it cannot be hidden. */}
        <div ref={canvasRef} className="tz-ui-map-canvas" aria-hidden={interactive ? undefined : true} />
        {ring}
        {projector && (
          <div className="tz-ui-map-overlay">
            {clusters.map((f) => {
              const [lng, lat] = f.geometry.coordinates as [number, number];
              const xy = projector.project({ lng, lat });
              if (!inBox(xy)) return null;
              const props = f.properties as Partial<ClusterProperties> & { id?: string };
              if (props.cluster && props.cluster_id !== undefined && props.point_count !== undefined) {
                const clusterId = props.cluster_id;
                return (
                  <button
                    key={`c${clusterId}`}
                    type="button"
                    className="tz-ui-map-cluster"
                    style={place(xy)}
                    aria-label={clusterLabel(props.point_count)}
                    onClick={() => openCluster(clusterId, { lng, lat })}
                  >
                    <span aria-hidden="true">{props.point_count_abbreviated ?? props.point_count}</span>
                  </button>
                );
              }
              const shop = props.id !== undefined ? byId.get(props.id) : undefined;
              if (!shop) return null;
              return (
                <button
                  key={shop.id}
                  type="button"
                  className="tz-ui-map-pin"
                  data-open={shop.open ? '' : undefined}
                  style={place(xy)}
                  aria-label={pinLabel(shop)}
                  onClick={() => onSelectShop(shop.id)}
                />
              );
            })}
            <span className="tz-ui-map-you" style={place(projector.project(center))} role="img" aria-label={youLabel} />
          </div>
        )}
        {liveLabel !== undefined && <span className="tz-map-live">{liveLabel}</span>}
        <span className="tz-map-legend">
          <span>{legend.open}</span>
          <span className="closed">{legend.closed}</span>
          <span className="you">{legend.you}</span>
        </span>
        {mode === 'webgl' && <span className="tz-ui-map-attribution">{attribution}</span>}
        {onExpand && expandLabel !== undefined && (
          <button type="button" className="tz-map-btn" aria-label={expandLabel} onClick={onExpand}>
            <Icon icon={Maximize2} />
          </button>
        )}
      </div>
      {stats && stats.length > 0 && (
        <div className="tz-map-stats">
          {stats.map((s) => (
            <div key={s.key}>
              <b>{s.value}</b>
              <small>{s.label}</small>
            </div>
          ))}
        </div>
      )}
      <div className="tz-map-radius">
        <RadiusChips label={radiusLabel} options={radiusOptions} value={radiusKm} onChange={onRadiusChange} />
      </div>
    </section>
  );
}
