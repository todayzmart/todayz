// "Book" on a Yes card (01 §11.2): the replied store item goes into the cart with the asked quantity and
// the reply's id, and checkout (C-11) prices it at the reply while that is valid. A reply whose price has
// expired books at the current price: the line carries no reply id, and checkout's "price changed" second
// tap shows the new price. One shop per cart: when the cart holds another shop's items, the same "replace
// cart" sheet as the shop page asks first.
import type { Ask } from '@todayz/api-client/gen/model/ask';
import type { AskReply } from '@todayz/api-client/gen/model/askReply';
import { useRouter } from '@tanstack/react-router';
import { Button, Sheet } from '@todayz/ui';
import { useState, type ReactNode } from 'react';
import { useIntl } from 'react-intl';
import { addToCart, getCart, replaceCart, setQty, type CartLine, type CartStore } from '../cart/cart';

interface Pending {
  store: CartStore;
  line: Omit<CartLine, 'qty'>;
  qty: number;
  current: CartStore;
}

/**
 * The cart line for a reply. The price is what the person saw on the reply; the quote decides what is
 * charged. A Yes without a price has no expiry and keeps the reply (catalogue price, the asked quantity
 * at most); an expired price drops it.
 */
export function lineFor(ask: Ask, reply: AskReply, priceValid: boolean): Omit<CartLine, 'qty'> | null {
  if (!reply.storeItemId) return null;
  const price = reply.pricePaise ?? reply.listedPricePaise;
  if (price === null) return null;
  const keepsReply = priceValid || reply.pricePaise === null;
  return {
    storeItemId: reply.storeItemId,
    productName: ask.itemName,
    pricePaise: price,
    ...(keepsReply ? { askReplyId: reply.askReplyId, askQty: ask.quantity } : {}),
    ...(keepsReply && reply.pricePaise !== null ? { askPriced: true as const } : {}),
  };
}

export function useBookReply(): { book: (ask: Ask, reply: AskReply, priceValid: boolean) => void; sheet: ReactNode } {
  const intl = useIntl();
  const router = useRouter();
  const [pending, setPending] = useState<Pending | null>(null);

  const book = (ask: Ask, reply: AskReply, priceValid: boolean) => {
    const line = lineFor(ask, reply, priceValid);
    if (!line) return;
    const store: CartStore = { id: reply.storeId, slug: reply.storeSlug, name: reply.storeName };
    const cart = getCart();
    if (cart.store && cart.store.id !== store.id) {
      setPending({ store, line, qty: ask.quantity, current: cart.store });
      return;
    }
    // The asked quantity, not one more on top of what is there.
    if (cart.lines.some((l) => l.storeItemId === line.storeItemId)) setQty(line.storeItemId, 0);
    addToCart(store, line, ask.quantity);
    router.history.push('/checkout');
  };

  const close = () => setPending(null);
  const sheet = (
    <Sheet
      open={pending !== null}
      onClose={close}
      title={intl.formatMessage({ id: 'store.replace.title' })}
      description={
        pending ? intl.formatMessage({ id: 'store.replace.body' }, { current: pending.current.name, next: pending.store.name }) : undefined
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
              if (pending) {
                replaceCart(pending.store, pending.line, pending.qty);
                router.history.push('/checkout');
              }
              close();
            }}
          >
            {intl.formatMessage({ id: 'store.replace.confirm' })}
          </Button>
        </>
      }
    />
  );
  return { book, sheet };
}
