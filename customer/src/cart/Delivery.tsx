// C-09 / C-11 delivery: the pickup/delivery switch with the shop's terms, and the quote's delivery
// refusals in words (01 §5.5, D-94, D-219). The radius, the minimum and the hours are the server's
// answer; this only says them.
import type { CheckoutQuote } from '@todayz/api-client/gen/model/checkoutQuote';
import type { DiscoverStorePage } from '@todayz/api-client/gen/model/discoverStorePage';
import type { NextDeliveryWindow } from '@todayz/api-client/gen/model/nextDeliveryWindow';
import { formatClock, formatDistance, formatMoney, istParts } from '@todayz/i18n';
import { Banner, Button, Segmented, type SegmentedOption } from '@todayz/ui';
import { Bike, ShoppingBag } from 'lucide-react';
import type { ReactNode } from 'react';
import { useIntl } from 'react-intl';
import { deliveryTerms, freeDeliveryGap, type DeliveryProblem } from './deliveryRules';
import type { Mode } from './fulfilment';
import styles from './tx.module.css';

/** "today 8:00 am" or "Mon 8:00 am", in IST, from the window's start against `now`. */
function useWindowText() {
  const intl = useIntl();
  return (w: NextDeliveryWindow, now: string | undefined): string => {
    const start = istParts(w.opensAt);
    const today = now ? istParts(now) : null;
    const time = `${formatClock(w.opensAt)}–${formatClock(w.closesAt)}`;
    if (today && today.dayNumber === start.dayNumber) return intl.formatMessage({ id: 'delivery.window.today' }, { time });
    if (today && today.dayNumber + 1 === start.dayNumber) return intl.formatMessage({ id: 'delivery.window.tomorrow' }, { time });
    const day = intl.formatDate(Date.parse(w.opensAt), { weekday: 'long', timeZone: 'Asia/Kolkata' });
    return intl.formatMessage({ id: 'delivery.window.day' }, { day, time });
  };
}

/** The delivery refusal with a way on: "Switch to pickup" (D-219), and the cart is kept. */
export function DeliveryProblemBanner({ problem, now, onPickup }: { problem: DeliveryProblem; now?: string | undefined; onPickup: () => void }) {
  const intl = useIntl();
  const windowText = useWindowText();
  const action = (
    <Button size="sm" variant="soft" onClick={onPickup}>
      {intl.formatMessage({ id: 'delivery.switchToPickup' })}
    </Button>
  );
  switch (problem.kind) {
    case 'delivery_closed':
      return (
        <Banner tone="warning" live="status" title={intl.formatMessage({ id: 'delivery.closed.title' })} action={action}>
          {problem.next
            ? intl.formatMessage({ id: 'delivery.closed.next' }, { window: windowText(problem.next, now) })
            : intl.formatMessage({ id: 'delivery.closed.none' })}
        </Banner>
      );
    case 'below_minimum': {
      const short = problem.itemSubtotalPaise === null ? null : Math.max(0, problem.minimumPaise - problem.itemSubtotalPaise);
      return (
        <Banner tone="warning" live="status" title={intl.formatMessage({ id: 'delivery.minimum.title' }, { amount: formatMoney(problem.minimumPaise) })} action={action}>
          {short !== null && short > 0
            ? intl.formatMessage({ id: 'delivery.minimum.addMore' }, { amount: formatMoney(short) })
            : intl.formatMessage({ id: 'delivery.minimum.body' })}
        </Banner>
      );
    }
    case 'outside_radius':
      return (
        <Banner tone="warning" live="status" title={intl.formatMessage({ id: 'delivery.outside.title' })} action={action}>
          {intl.formatMessage({ id: 'delivery.outside.body' })}
        </Banner>
      );
  }
}

/**
 * The pickup/delivery switch (C-09, C-11). Shown only when the shop delivers; otherwise one line says
 * the order is picked up. `children` is what goes under it for delivery: the address summary in the
 * cart, the address picker at checkout.
 */
export function FulfilmentSwitch({
  store,
  offered,
  mode,
  quote,
  onChange,
  children,
}: {
  store: DiscoverStorePage | undefined;
  offered: boolean;
  mode: Mode;
  quote: CheckoutQuote | undefined;
  onChange: (mode: Mode) => void;
  children?: ReactNode;
}) {
  const intl = useIntl();
  if (!offered || !store) return <p className={styles.meta}>{intl.formatMessage({ id: 'cart.pickupOnly' })}</p>;
  const options: readonly SegmentedOption<Mode>[] = [
    { value: 'pickup', label: intl.formatMessage({ id: 'delivery.mode.pickup' }), icon: ShoppingBag },
    { value: 'delivery', label: intl.formatMessage({ id: 'delivery.mode.delivery' }), icon: Bike },
  ];
  const gap = freeDeliveryGap(store, quote);
  return (
    <div className={styles.stack}>
      <Segmented className={styles.mode} label={intl.formatMessage({ id: 'delivery.mode.label' })} options={options} value={mode} onChange={onChange} />
      {mode === 'pickup' ? (
        <p className={styles.meta}>{intl.formatMessage({ id: 'cart.pickupOnly' })}</p>
      ) : (
        <>
          <p className={styles.meta}>
            {deliveryTerms(store, intl)}
            {store.deliveryRadiusM > 0 && (
              <>
                {' · '}
                {intl.formatMessage({ id: 'delivery.terms.radius' }, { distance: formatDistance(store.deliveryRadiusM) })}
              </>
            )}
          </p>
          {gap !== null && <p className={styles.meta}>{intl.formatMessage({ id: 'delivery.freeGap' }, { amount: formatMoney(gap) })}</p>}
          {children}
        </>
      )}
    </div>
  );
}
