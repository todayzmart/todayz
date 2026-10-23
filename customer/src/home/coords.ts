// The point every discover call needs. A GPS place already has one; a PIN code is looked up once with
// `searchPlaces` (C-02 keeps only the PIN). Nothing here stores or logs the point.
import { searchPlaces } from '@todayz/api-client/gen/discover/discover';
import { useQuery } from '@tanstack/react-query';
import { usePlace } from '../app/place';

export interface Coords {
  lat: number;
  lng: number;
}

export type CoordsState =
  | { status: 'none'; coords: null }
  | { status: 'loading'; coords: null }
  | { status: 'error'; coords: null; retry: () => void }
  | { status: 'ready'; coords: Coords };

export function useCoords(): CoordsState {
  const place = usePlace();
  const pin = place?.kind === 'pin' ? place.pin : null;
  const lookup = useQuery({
    queryKey: ['discover', 'pin', pin],
    enabled: pin !== null,
    staleTime: 24 * 60 * 60_000,
    queryFn: async () => {
      const r = await searchPlaces({ q: pin ?? '', limit: 1 });
      const first = r.items[0];
      if (!first) throw new Error('pin_unknown');
      return { lat: first.lat, lng: first.lng } satisfies Coords;
    },
  });
  if (!place) return { status: 'none', coords: null };
  if (place.kind === 'gps') return { status: 'ready', coords: { lat: place.lat, lng: place.lng } };
  if (lookup.data) return { status: 'ready', coords: lookup.data };
  if (lookup.isError) return { status: 'error', coords: null, retry: () => void lookup.refetch() };
  return { status: 'loading', coords: null };
}
