// Add to cart from the shop page, search results and the product page. One shop at a time: when the
// cart holds another shop's items, a sheet asks whether to replace it (C-09). The cart module is W2-09's;
// this file only calls its exported functions.
import type { DiscoverSearchItem } from '@todayz/api-client/gen/model/discoverSearchItem';
import { Button, Sheet } from '@todayz/ui';
import { useState, type ReactNode } from 'react';
import { useIntl } from 'react-intl';
import { imageUrl } from '../home/present';
import { addToCart, replaceCart, setQty, useCart, type CartLine, type CartStore } from '../cart/cart';

export interface ShopRef {
  /** The store ULID: what checkout (CheckoutQuoteRequest.storeId) needs. */
  id: string;
  slug: string;
  name: string;
}

interface Pending {
  shop: CartStore;
  line: Omit<CartLine, 'qty'>;
  qty: number;
  current: CartStore;
}

function toStore(shop: ShopRef): CartStore {
  return { id: shop.id, slug: shop.slug, name: shop.name };
}

export function lineOf(item: DiscoverSearchItem, pack: string): Omit<CartLine, 'qty'> {
  const line: Omit<CartLine, 'qty'> = {
    storeItemId: item.storeItemId,
    productName: item.name,
    packLabel: pack,
    pricePaise: item.pricePaise,
  };
  const img = imageUrl(item.imageKey);
  if (img) line.imageUrl = img;
  if (item.mrpPaise !== null && item.mrpPaise > item.pricePaise) line.mrpPaise = item.mrpPaise;
  return line;
}

export function useCartActions(): {
  qtyOf: (shopSlug: string, storeItemId: string) => number;
  change: (shop: ShopRef, item: DiscoverSearchItem, pack: string, qty: number) => void;
  sheet: ReactNode;
} {
  const intl = useIntl();
  const cart = useCart();
  const [pending, setPending] = useState<Pending | null>(null);

  const qtyOf = (shopSlug: string, storeItemId: string): number =>
    cart.store?.slug === shopSlug ? (cart.lines.find((l) => l.storeItemId === storeItemId)?.qty ?? 0) : 0;

  const change = (shop: ShopRef, item: DiscoverSearchItem, pack: string, qty: number): void => {
    if (qtyOf(shop.slug, item.storeItemId) > 0) {
      setQty(item.storeItemId, qty);
      return;
    }
    if (qty <= 0) return;
    const store = toStore(shop);
    const line = lineOf(item, pack);
    const r = addToCart(store, line, qty);
    if (!r.ok) setPending({ shop: store, line, qty, current: r.current });
  };

  const close = () => setPending(null);
  const sheet = (
    <Sheet
      open={pending !== null}
      onClose={close}
      title={intl.formatMessage({ id: 'store.replace.title' })}
      description={
        pending
          ? intl.formatMessage({ id: 'store.replace.body' }, { current: pending.current.name, next: pending.shop.name })
          : undefined
      }
      closeLabel={intl.formatMessage({ id: 'store.replace.close' })}
      footer={
        <>
          <Button variant="ghost" onClick={close}>
            {intl.formatMessage({ id: 'store.replace.keep' })}
          </Button>
          <Button
            variant="primary"
            onClick={() => {
              if (pending) replaceCart(pending.shop, pending.line, pending.qty);
              close();
            }}
          >
            {intl.formatMessage({ id: 'store.replace.confirm' })}
          </Button>
        </>
      }
    />
  );

  return { qtyOf, change, sheet };
}
