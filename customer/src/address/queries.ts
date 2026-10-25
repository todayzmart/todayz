// Saved addresses (W2-15; C-11, C-18). The list is read once per screen and refreshed after every
// change. The text is the person's own data: it lives only in the query cache, never in a URL,
// storage or a log.
import type { Address } from '@todayz/api-client/gen/model/address';
import type { AddressCreate } from '@todayz/api-client/gen/model/addressCreate';
import { createAddress, deleteAddress, listMyAddresses, updateAddress } from '@todayz/api-client/gen/me/me';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { newUlid } from '../orders/ulid';

export const ADDRESSES_KEY = ['me', 'addresses'] as const;

export function useAddresses(enabled: boolean) {
  return useQuery({ queryKey: ADDRESSES_KEY, enabled, queryFn: () => listMyAddresses(), staleTime: 30_000 });
}

/** `If-Match` from the version the list showed (the server's ETag is `"v{version}"`). */
const tagOf = (a: Address) => `"v${a.version}"`;

export function useAddressActions() {
  const client = useQueryClient();
  const refresh = () => client.invalidateQueries({ queryKey: ADDRESSES_KEY });
  const create = useMutation({
    mutationFn: (body: AddressCreate) => createAddress(body, { idempotencyKey: newUlid() }),
    onSuccess: refresh,
  });
  const remove = useMutation({ mutationFn: (a: Address) => deleteAddress(a.addressId), onSuccess: refresh });
  const makeDefault = useMutation({
    mutationFn: (a: Address) => updateAddress(a.addressId, { isDefault: true }, { ifMatch: tagOf(a) }),
    // A 412 means the list was stale: reading it again is the fix either way.
    onSettled: refresh,
  });
  return { create, remove, makeDefault };
}
