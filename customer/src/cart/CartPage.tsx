import { ApiError } from '@todayz/api-client';
import type { QuoteLine } from '@todayz/api-client/gen/model/quoteLine';
import { useSessionState } from '@todayz/auth';
import { formatMoney } from '@todayz/i18n';
import { useRouter } from '@tanstack/react-router';
import { Banner, Button, CartBar, EmptyState, QuantityStepper, Skeleton } from '@todayz/ui';
import { ShoppingBag } from 'lucide-react';
import { useIntl } from 'react-intl';
import { PathLink } from '../orders/ShopLink';
import { cartCount, clearCart, setQty, useCart, type CartLine } from './cart';
import { useCartQuote } from './quote';
import styles from './tx.module.css';

/** Problem codes the quote can answer with that the cart explains in words. */
function useQuoteProblemText() {
  const intl = useIntl();
  return (err: unknown): string => {
    const code = err instanceof ApiError ? err.code : 'other';
    switch (code) {
      case 'store_closed':
      case 'store_not_bookable':
      case 'ordering_suspended':
      case 'account_has_active_order':
        return intl.formatMessage({ id: `checkout.problem.${code}` });
      default:
        return intl.formatMessage({ id: 'cart.quoteError' });
    }
  };
}

/** C-09 Cart: one shop, lines with steppers, unavailable lines flagged, totals, sticky checkout bar. */
export function CartPage() {
  const intl = useIntl();
  const cart = useCart();
  const router = useRouter();
  const { status } = useSessionState();
  const quote = useCartQuote(cart, status === 'signedIn');
  const problemText = useQuoteProblemText();

  if (!cart.store || cart.lines.length === 0) {
    return (
      <div className={styles.page}>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'cart.title' })}</h1>
        <EmptyState
          icon={ShoppingBag}
          title={intl.formatMessage({ id: 'cart.empty.title' })}
          description={intl.formatMessage({ id: 'cart.empty.body' })}
          action={
            <PathLink to="/" className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'cart.empty.cta' })}
            </PathLink>
          }
        />
      </div>
    );
  }

  const byId = new Map<string, QuoteLine>((quote.data?.lines ?? []).map((l) => [l.storeItemId, l]));
  const blocked = (quote.data && !quote.data.orderable) === true;
  const localTotal = cart.lines.reduce((sum, l) => sum + l.pricePaise * l.qty, 0);
  const total = quote.data ? quote.data.totalPaise : localTotal;

  return (
    <div className={`${styles.page} ${styles.withBar}`}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'cart.title' })}</h1>
      <p className={styles.meta}>
        <PathLink to={`/s/${cart.store.slug}`} className={styles.textLink}>{intl.formatMessage({ id: 'cart.from' }, { shop: cart.store.name })}</PathLink>
      </p>

      {quote.isError && (
        <Banner tone="warning" live="status">
          {problemText(quote.error)}
        </Banner>
      )}
      {blocked && (
        <Banner tone="warning" title={intl.formatMessage({ id: 'cart.unavailable.title' })}>
          {intl.formatMessage({ id: 'cart.unavailable.body' })}
        </Banner>
      )}

      <section className="tz-card" aria-label={intl.formatMessage({ id: 'cart.lines.label' })}>
        {quote.isLoading && (
          <div role="status" aria-label={intl.formatMessage({ id: 'cart.loading' })}>
            <Skeleton height="4rem" />
          </div>
        )}
        <ul className={styles.list}>
          {cart.lines.map((l) => (
            <CartRow key={l.storeItemId} line={l} quoted={byId.get(l.storeItemId)} />
          ))}
        </ul>
      </section>

      <section className="tz-card">
        <dl className={styles.totals}>
          {quote.data && quote.data.discountPaise > 0 && (
            <>
              <div className={styles.totalRow}>
                <dt>{intl.formatMessage({ id: 'cart.subtotal' })}</dt>
                <dd>{formatMoney(quote.data.subtotalPaise)}</dd>
              </div>
              <div className={styles.totalRow}>
                <dt>{intl.formatMessage({ id: 'cart.discount' })}</dt>
                <dd>−{formatMoney(quote.data.discountPaise)}</dd>
              </div>
            </>
          )}
          <div className={`${styles.totalRow} ${styles.grand}`}>
            <dt>{intl.formatMessage({ id: 'cart.total' })}</dt>
            <dd>{formatMoney(total)}</dd>
          </div>
        </dl>
        {status !== 'signedIn' && <p className={styles.meta}>{intl.formatMessage({ id: 'cart.priceNote' })}</p>}
        <p className={styles.meta}>{intl.formatMessage({ id: 'cart.pickupOnly' })}</p>
        {/* TODO(W2-03): the pickup/delivery switch appears here only when the quote says delivery is possible. */}
        {/* TODO(W2-01): the credits toggle and amount slider are hidden; credits are not built. */}
      </section>

      <Button variant="ghost" onClick={() => clearCart()}>
        {intl.formatMessage({ id: 'cart.clear' })}
      </Button>

      {!blocked && (
        <div className={styles.sticky}>
          <CartBar
            count={cartCount(cart)}
            countLabel={intl.formatMessage({ id: 'cart.count' }, { count: cartCount(cart) })}
            total={formatMoney(total)}
            from={intl.formatMessage({ id: 'cart.from' }, { shop: cart.store.name })}
            ctaLabel={intl.formatMessage({ id: 'cart.checkout' })}
            onOpen={() => router.history.push('/checkout')}
          />
        </div>
      )}
    </div>
  );
}

function CartRow({ line, quoted }: { line: CartLine; quoted: QuoteLine | undefined }) {
  const intl = useIntl();
  const price = quoted?.unitPricePaise ?? line.pricePaise;
  const mrp = quoted?.mrpPaise ?? line.mrpPaise;
  const short = quoted && quoted.availability === 'short';
  const out = quoted && quoted.availability === 'out_of_stock';
  const pack = line.packLabel ?? (quoted ? [quoted.variant, `${quoted.packSize} ${quoted.unit}`].filter(Boolean).join(' · ') : undefined);
  return (
    <li className={styles.line}>
      <div>
        <p className={styles.name}>{line.productName}</p>
        {pack && <p className={styles.meta}>{pack}</p>}
        <p className={styles.meta}>
          {formatMoney(price)}
          {mrp !== undefined && mrp !== null && mrp > price && (
            <>
              {' '}
              <span className={styles.strike}>{formatMoney(mrp)}</span>
            </>
          )}
        </p>
        {out && <p className={styles.flag}>{intl.formatMessage({ id: 'cart.line.out' })}</p>}
        {short && quoted && (
          <p className={styles.flag}>{intl.formatMessage({ id: 'cart.line.short' }, { count: quoted.availableQuantity })}</p>
        )}
      </div>
      <div className={styles.stack}>
        <QuantityStepper
          alwaysStepper
          quantity={line.qty}
          max={99}
          itemName={line.productName}
          onChange={(q) => setQty(line.storeItemId, q)}
          labels={{
            add: intl.formatMessage({ id: 'cart.stepper.add' }),
            decrement: intl.formatMessage({ id: 'cart.stepper.remove' }),
            increment: intl.formatMessage({ id: 'cart.stepper.increment' }),
          }}
        />
        {(out || short) && (
          <Button size="sm" variant="soft-danger" onClick={() => setQty(line.storeItemId, short && quoted ? quoted.availableQuantity : 0)}>
            {short && quoted && quoted.availableQuantity > 0
              ? intl.formatMessage({ id: 'cart.line.reduce' }, { count: quoted.availableQuantity })
              : intl.formatMessage({ id: 'cart.line.remove' })}
          </Button>
        )}
      </div>
    </li>
  );
}
