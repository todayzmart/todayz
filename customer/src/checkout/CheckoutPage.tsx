import { ApiError } from '@todayz/api-client';
import type { CheckoutQuote } from '@todayz/api-client/gen/model/checkoutQuote';
import { getDiscoverStore } from '@todayz/api-client/gen/discover/discover';
import { createOrder } from '@todayz/api-client/gen/orders/orders';
import { SessionPending, useHoldUpdates, useSessionState } from '@todayz/auth';
import { formatMoney } from '@todayz/i18n';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useRouter } from '@tanstack/react-router';
import { Banner, Button, EmptyState, Skeleton } from '@todayz/ui';
import { Banknote, ShoppingBag } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useIntl } from 'react-intl';
import { useUpdateController } from '../app/update-context';
import { clearCart, useCart } from '../cart/cart';
import { CreditsControl } from '../cart/CreditsControl';
import { FULFILMENT, quoteKey, todaysHours, useCartPricing } from '../cart/quote';
import styles from '../cart/tx.module.css';
import { PathLink } from '../orders/ShopLink';
import { newUlid } from '../orders/ulid';
import { Icon } from '../shell/Icon';

type PlaceProblem =
  | { kind: 'price_changed' }
  | { kind: 'insufficient_stock'; storeItemId?: string; available?: number }
  | { kind: 'store_closed' }
  | { kind: 'until'; code: 'ordering_suspended'; until?: string }
  | { kind: 'code'; code: string }
  | { kind: 'network' };

function problemOf(err: unknown): PlaceProblem {
  if (!(err instanceof ApiError)) return { kind: 'network' };
  const ext = err.problem.extensions ?? {};
  switch (err.code) {
    case 'price_changed':
    case 'ask_price_expired':
      return { kind: 'price_changed' };
    case 'insufficient_stock':
      return {
        kind: 'insufficient_stock',
        ...(typeof ext['storeItemId'] === 'string' ? { storeItemId: ext['storeItemId'] } : {}),
        ...(typeof ext['available'] === 'number' ? { available: ext['available'] } : {}),
      };
    case 'store_closed':
      return { kind: 'store_closed' };
    case 'ordering_suspended':
      return { kind: 'until', code: 'ordering_suspended', ...(typeof ext['until'] === 'string' ? { until: ext['until'] } : {}) };
    default:
      return err.status >= 500 ? { kind: 'network' } : { kind: 'code', code: err.code };
  }
}

/** C-11 Checkout (pickup): review, the pay-at-the-shop note, total, Place order. */
export function CheckoutPage() {
  const intl = useIntl();
  const router = useRouter();
  const client = useQueryClient();
  const cart = useCart();
  const { status } = useSessionState();
  const update = useUpdateController();
  // G9, D-98: the "new version" prompt waits while a checkout is open.
  useHoldUpdates(update, true);

  const { quote, probe, choice, settling } = useCartPricing(cart, status === 'signedIn');
  const [accepted, setAccepted] = useState<number | null>(null);
  const [problem, setProblem] = useState<PlaceProblem | null>(null);
  const [placing, setPlacing] = useState(false);
  const attempt = useRef<{ sig: string; key: string } | null>(null);
  const [placed, setPlaced] = useState(false);

  // Signed out: sign in first, then back here with the cart kept (it lives on the device).
  useEffect(() => {
    if (status === 'signedOut' && !placed) router.history.replace('/sign-in?next=%2Fcheckout');
  }, [status, placed, router]);

  // The total the person has agreed to: set at once when no price moved since the cart was filled,
  // else by the first tap on the button.
  const q = quote.data;
  const repriced = q ? priceMoves(cart.lines, q) : [];
  const agreed = accepted ?? (q && repriced.length === 0 ? q.totalPaise : null);

  const closed = problem?.kind === 'store_closed' || (quote.error instanceof ApiError && quote.error.code === 'store_closed');
  const hours = useQuery({
    queryKey: ['store-hours', cart.store?.slug ?? ''],
    enabled: closed && cart.store !== null,
    queryFn: () => getDiscoverStore(cart.store?.slug ?? ''),
    staleTime: 60_000,
  });

  if (status === 'unknown') {
    return (
      <SessionPending
        className={styles.page}
        loadingLabel={intl.formatMessage({ id: 'checkout.loading' })}
        offlineTitle={intl.formatMessage({ id: 'app.offline.title' })}
        offlineMessage={intl.formatMessage({ id: 'app.offline.body' })}
        retryLabel={intl.formatMessage({ id: 'app.pending.retry' })}
      />
    );
  }
  if (status === 'signedOut') return null;

  if (!cart.store || cart.lines.length === 0) {
    if (placed) return null;
    return (
      <div className={styles.page}>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'checkout.title' })}</h1>
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
  const store = cart.store;

  const needsAccept = q !== undefined && agreed !== q.totalPaise;
  const blocked = q !== undefined && !q.orderable;

  const place = async () => {
    if (!q) return;
    if (needsAccept) {
      // The second tap: the new total is agreed, nothing is sent yet.
      setAccepted(q.totalPaise);
      setProblem(null);
      return;
    }
    const items = cart.lines.map((l) => ({ storeItemId: l.storeItemId, quantity: l.qty }));
    const body = {
      storeId: store.id,
      fulfilment: FULFILMENT,
      items,
      creditsToUse: q.creditsUsed,
      expectedTotalPaise: q.totalPaise,
    };
    // One key per attempt; a retry of the same order reuses it so the shop gets one order (BK-04).
    const sig = JSON.stringify(body);
    if (attempt.current?.sig !== sig) attempt.current = { sig, key: newUlid() };
    setPlacing(true);
    setProblem(null);
    try {
      const order = await createOrder(body, { idempotencyKey: attempt.current.key });
      setPlaced(true);
      attempt.current = null;
      router.history.push(`/orders/${order.id}?placed=1`);
      clearCart();
      void client.invalidateQueries({ queryKey: ['orders'] });
    } catch (err) {
      const p = problemOf(err);
      setProblem(p);
      if (p.kind === 'price_changed' && err instanceof ApiError) {
        // The 409 carries the fresh quote: show it and wait for a second tap.
        const fresh = err.problem.extensions?.['quote'] as CheckoutQuote | undefined;
        if (fresh) client.setQueryData(quoteKey(cart, q?.creditsRequested ?? 0), fresh);
        else void client.invalidateQueries({ queryKey: quoteKey(cart, q?.creditsRequested ?? 0) });
      } else if (p.kind === 'insufficient_stock') {
        void client.invalidateQueries({ queryKey: ['quote'] });
      }
    } finally {
      setPlacing(false);
    }
  };

  const nameOf = (id?: string) => cart.lines.find((l) => l.storeItemId === id)?.productName;

  return (
    <div className={`${styles.page} ${styles.withBar}`}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'checkout.title' })}</h1>

      <section className="tz-card" aria-label={intl.formatMessage({ id: 'checkout.pickup.label' })}>
        <h2 className="tz-card-title">{intl.formatMessage({ id: 'checkout.pickup.title' })}</h2>
        <p className={styles.meta}>{store.name}</p>
        {/* TODO(W2-03): delivery and the address picker. Pickup only this weekend. */}
      </section>

      {closed && (
        <Banner tone="warning" title={intl.formatMessage({ id: 'checkout.problem.store_closed' })}>
          {hours.data
            ? (() => {
                const today = todaysHours(hours.data.hours.weekly, hours.data.serverNow);
                return today.length > 0
                  ? intl.formatMessage({ id: 'checkout.hoursToday' }, { hours: today.join(', ') })
                  : intl.formatMessage({ id: 'checkout.closedToday' });
              })()
            : intl.formatMessage({ id: 'checkout.cartKept' })}
        </Banner>
      )}
      {quote.error instanceof ApiError && quote.error.code !== 'store_closed' && (
        <Banner tone="warning" live="status">
          {intl.formatMessage({
            id: ['store_not_bookable', 'ordering_suspended', 'account_has_active_order'].includes(quote.error.code)
              ? `checkout.problem.${quote.error.code}`
              : 'cart.quoteError',
          })}
        </Banner>
      )}
      {repriced.length > 0 && needsAccept && !problem && (
        <Banner tone="warning" live="status" title={intl.formatMessage({ id: 'checkout.priceChanged.title' })}>
          {intl.formatMessage({ id: 'checkout.priceChanged.body' })}
        </Banner>
      )}
      {problem?.kind === 'price_changed' && (
        <Banner tone="warning" live="status" title={intl.formatMessage({ id: 'checkout.priceChanged.title' })}>
          {intl.formatMessage({ id: 'checkout.priceChanged.body' })}
        </Banner>
      )}
      {problem?.kind === 'insufficient_stock' && (
        <Banner tone="warning" live="status" title={intl.formatMessage({ id: 'checkout.stock.title' })}>
          {intl.formatMessage(
            { id: 'checkout.stock.body' },
            { item: nameOf(problem.storeItemId) ?? intl.formatMessage({ id: 'checkout.stock.anItem' }), count: problem.available ?? 0 },
          )}{' '}
          <PathLink to="/cart" className={styles.textLink}>{intl.formatMessage({ id: 'checkout.stock.fix' })}</PathLink>
        </Banner>
      )}
      {problem?.kind === 'until' && (
        <Banner tone="warning" live="status">
          {intl.formatMessage({ id: 'checkout.problem.ordering_suspended' })}
        </Banner>
      )}
      {problem?.kind === 'code' && (
        <Banner tone="warning" live="status">
          {intl.formatMessage({
            id: ['store_not_bookable', 'account_has_active_order'].includes(problem.code)
              ? `checkout.problem.${problem.code}`
              : 'checkout.problem.other',
          })}
        </Banner>
      )}
      {problem?.kind === 'network' && (
        <Banner tone="warning" live="status">
          {intl.formatMessage({ id: 'checkout.problem.network' })}
        </Banner>
      )}

      <section className="tz-card" aria-label={intl.formatMessage({ id: 'checkout.review.label' })}>
        <h2 className="tz-card-title">{intl.formatMessage({ id: 'checkout.review.title' })}</h2>
        {quote.isLoading && (
          <div role="status" aria-label={intl.formatMessage({ id: 'checkout.loading' })}>
            <Skeleton height="4rem" />
          </div>
        )}
        <ul className={styles.list}>
          {(q ? q.lines : []).map((l) => (
            <li key={l.storeItemId} className={styles.line}>
              <div>
                <p className={styles.name}>
                  {l.name} × {l.quantity}
                </p>
                <p className={styles.meta}>{[l.variant, `${l.packSize} ${l.unit}`].filter(Boolean).join(' · ')}</p>
                {l.availability !== 'in_stock' && (
                  <p className={styles.flag}>
                    {l.availability === 'out_of_stock'
                      ? intl.formatMessage({ id: 'cart.line.out' })
                      : intl.formatMessage({ id: 'cart.line.short' }, { count: l.availableQuantity })}
                  </p>
                )}
              </div>
              <span className={styles.price}>{formatMoney(l.lineTotalPaise)}</span>
            </li>
          ))}
        </ul>
        {q && (
          <dl className={styles.totals}>
            {q.discountPaise > 0 && (
              <div className={styles.totalRow}>
                <dt>{intl.formatMessage({ id: 'cart.discount' })}</dt>
                <dd>−{formatMoney(q.discountPaise)}</dd>
              </div>
            )}
            {q.creditsUsed > 0 && (
              <div className={styles.totalRow}>
                <dt>{intl.formatMessage({ id: 'cart.creditsRow' })}</dt>
                <dd>−{formatMoney(q.creditsUsed * 100)}</dd>
              </div>
            )}
            <div className={`${styles.totalRow} ${styles.grand}`}>
              <dt>{intl.formatMessage({ id: 'cart.total' })}</dt>
              <dd>{formatMoney(q.totalPaise)}</dd>
            </div>
          </dl>
        )}
        <CreditsControl storeId={store.id} probe={probe} quote={q} on={choice.on} amount={choice.amount} />
      </section>

      <p className={styles.note}>
        <Icon icon={Banknote} />
        <span>{intl.formatMessage({ id: 'checkout.payNote' })}</span>
      </p>

      <div className={styles.placeBar}>
        <Button
          variant="brand"
          size="lg"
          block
          loading={placing}
          loadingLabel={intl.formatMessage({ id: 'checkout.placing' })}
          disabled={!q || blocked || closed || settling}
          onClick={() => void place()}
        >
          {needsAccept && q
            ? intl.formatMessage({ id: 'checkout.accept' }, { total: formatMoney(q.totalPaise) })
            : intl.formatMessage({ id: 'checkout.place' }, { total: q ? formatMoney(q.totalPaise) : '' })}
        </Button>
        {blocked && <p className={styles.flag}>{intl.formatMessage({ id: 'cart.unavailable.body' })}</p>}
      </div>
    </div>
  );
}

/** Lines whose price now differs from the one the cart saved when the item was added. */
function priceMoves(lines: readonly { storeItemId: string; pricePaise: number }[], q: CheckoutQuote): string[] {
  const now = new Map(q.lines.map((l) => [l.storeItemId, l.unitPricePaise]));
  return lines.filter((l) => now.has(l.storeItemId) && now.get(l.storeItemId) !== l.pricePaise).map((l) => l.storeItemId);
}
