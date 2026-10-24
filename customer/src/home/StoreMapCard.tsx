// The StoreMap (C-03 card, C-04 full map): MapLibre is loaded lazily by the screens that import this file,
// so the initial bundle never carries it. The pins come from the discover summary; names are joined from the
// stores list the screen already loaded, with a generic name for a pin beyond it.
import type { DiscoverSummary } from '@todayz/api-client/gen/model/discoverSummary';
import { buildMapStyle, StoreMap, type MapShop } from '@todayz/ui/map';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';
import type { Coords } from './coords';
import { RADIUS_OPTIONS_KM, setRadiusKm } from './radius';
import { tilesUrlFor } from './tiles';

export interface StoreMapCardProps {
  coords: Coords;
  km: number;
  summary: DiscoverSummary | undefined;
  /** Shop names by slug, from the stores list. */
  names: ReadonlyMap<string, string>;
  /** false on Home (a tap on the card opens the full map); true on C-04. */
  interactive: boolean;
  height?: number | undefined;
  onExpand?: (() => void) | undefined;
  onSelectShop: (slug: string) => void;
}

export default function StoreMapCard({ coords, km, summary, names, interactive, height, onExpand, onSelectShop }: StoreMapCardProps) {
  const intl = useIntl();
  const fallback = intl.formatMessage({ id: 'home.map.shopFallback' });
  const shops: MapShop[] = useMemo(
    () => (summary?.pins ?? []).map((p) => ({ id: p.storeSlug, name: names.get(p.storeSlug) ?? fallback, lat: p.lat, lng: p.lng, open: p.open })),
    [summary, names, fallback],
  );
  const center = useMemo(() => ({ lat: coords.lat, lng: coords.lng }), [coords.lat, coords.lng]);
  const tiles = tilesUrlFor(coords.lat, coords.lng);
  const mapStyle = useMemo(() => (tiles ? buildMapStyle({ tilesUrl: tiles }) : undefined), [tiles]);
  const num = (n: number) => n.toLocaleString('en-IN');
  const total = summary ? num(summary.storesNearby) : '0';
  const open = summary ? num(summary.openNow) : '0';

  return (
    <StoreMap
      label={intl.formatMessage({ id: 'home.map.label' })}
      mapLabel={intl.formatMessage({ id: 'home.map.mapLabel' }, { shops: total, km: String(km), open })}
      center={center}
      shops={shops}
      radiusKm={km}
      radiusOptions={RADIUS_OPTIONS_KM.map((k) => ({ km: k, label: intl.formatMessage({ id: 'home.radius.km' }, { km: String(k) }) }))}
      radiusLabel={intl.formatMessage({ id: 'home.radius.label' })}
      onRadiusChange={setRadiusKm}
      onSelectShop={onSelectShop}
      pinLabel={(s) => intl.formatMessage({ id: s.open ? 'home.map.pinOpen' : 'home.map.pinClosed' }, { name: s.name })}
      clusterLabel={(count) => intl.formatMessage({ id: 'home.map.cluster' }, { count: String(count) })}
      youLabel={intl.formatMessage({ id: 'home.map.you' })}
      legend={{
        open: intl.formatMessage({ id: 'home.map.legend.open' }),
        closed: intl.formatMessage({ id: 'home.map.legend.closed' }),
        you: intl.formatMessage({ id: 'home.map.legend.you' }),
      }}
      liveLabel={intl.formatMessage({ id: 'home.map.live' })}
      stats={
        summary
          ? [
              { key: 'shops', value: total, label: intl.formatMessage({ id: 'home.stats.shops' }) },
              { key: 'open', value: open, label: intl.formatMessage({ id: 'home.stats.open' }) },
            ]
          : undefined
      }
      {...(onExpand ? { expandLabel: intl.formatMessage({ id: 'home.map.expand' }), onExpand } : {})}
      {...(mapStyle ? { mapStyle } : {})}
      height={height}
      interactive={interactive}
    />
  );
}
