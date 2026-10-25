// The address pin on the map (C-11): the pin is "you", the shop is the one marker and the ring is
// the shop's delivery radius drawn around the pin (the shop inside the ring is the pin inside the
// shop's radius). MapLibre loads lazily with this file. StoreMap has no drag or map-moved event, so
// the pin is moved with "Use my current location", not by dragging (W2-15 report).
import type { DiscoverStorePage } from '@todayz/api-client/gen/model/discoverStorePage';
import { formatDistance } from '@todayz/i18n';
import { buildMapStyle, StoreMap } from '@todayz/ui/map';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';
import { tilesUrlFor } from '../home/tiles';

export default function PinMap({ pin, store }: { pin: { lat: number; lng: number }; store: DiscoverStorePage }) {
  const intl = useIntl();
  const center = useMemo(() => ({ lat: pin.lat, lng: pin.lng }), [pin.lat, pin.lng]);
  const shops = useMemo(
    () => [{ id: store.storeSlug, name: store.name, lat: store.lat, lng: store.lng, open: store.open }],
    [store.storeSlug, store.name, store.lat, store.lng, store.open],
  );
  const tiles = tilesUrlFor(pin.lat, pin.lng);
  const mapStyle = useMemo(() => (tiles ? buildMapStyle({ tilesUrl: tiles }) : undefined), [tiles]);
  const km = store.deliveryRadiusM / 1000;
  const distance = formatDistance(store.deliveryRadiusM);
  return (
    <StoreMap
      label={intl.formatMessage({ id: 'address.map.label' })}
      mapLabel={intl.formatMessage({ id: 'address.map.mapLabel' }, { shop: store.name, distance })}
      center={center}
      shops={shops}
      radiusKm={km}
      radiusOptions={[{ km, label: distance }]}
      radiusLabel={intl.formatMessage({ id: 'address.map.radius' })}
      onRadiusChange={() => undefined}
      onSelectShop={() => undefined}
      pinLabel={(s) => s.name}
      clusterLabel={(count) => intl.formatMessage({ id: 'address.map.cluster' }, { count: String(count) })}
      youLabel={intl.formatMessage({ id: 'address.map.pin' })}
      legend={{
        open: intl.formatMessage({ id: 'address.map.legend.shop' }),
        closed: intl.formatMessage({ id: 'address.map.legend.shop' }),
        you: intl.formatMessage({ id: 'address.map.pin' }),
      }}
      {...(mapStyle ? { mapStyle } : {})}
      height={180}
      interactive={false}
    />
  );
}
