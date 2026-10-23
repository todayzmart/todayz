// @todayz/ui/map: the store map is a separate entry so screens without a map never pull in
// MapLibre (it is also loaded lazily inside the component).
export { StoreMap, type MapShop, type StoreMapProps } from './StoreMap';
export { buildMapStyle, fixtureMapStyle, mapColors, type MapStyleOptions } from './style';
export {
  circlePolygon,
  destination,
  pixelsPerMetre,
  radiusBounds,
  staticProjector,
  zoomToFitRadius,
  type LngLat,
  type Projector,
} from './geo';
export { hasWebGL, loadMapLibre } from './loader';
