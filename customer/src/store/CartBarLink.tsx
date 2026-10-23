// The floating cart bar on discover screens. The cart page itself is W2-09's (`/cart`).
import { CartBar } from '@todayz/ui';
import { useRouter } from '@tanstack/react-router';
import { useIntl } from 'react-intl';
import { cartCount, useCart } from '../cart/cart';
import { useFmt } from '../home/format';
import styles from './store.module.css';

export function CartBarLink() {
  const intl = useIntl();
  const router = useRouter();
  const cart = useCart();
  const { money } = useFmt();
  const count = cartCount(cart);
  if (count === 0 || !cart.store) return null;
  const total = cart.lines.reduce((sum, l) => sum + l.pricePaise * l.qty, 0);
  return (
    <div className={styles.cartbar}>
      <CartBar
        count={count}
        countLabel={intl.formatMessage({ id: 'store.cart.count' }, { count: String(count) })}
        total={money(total)}
        from={intl.formatMessage({ id: 'store.cart.from' }, { shop: cart.store.name })}
        ctaLabel={intl.formatMessage({ id: 'store.cart.open' })}
        onOpen={() => router.history.push('/cart')}
      />
    </div>
  );
}
