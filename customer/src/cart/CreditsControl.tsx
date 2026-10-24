import { formatMoney } from '@todayz/i18n';
import { useIntl } from 'react-intl';
import { creditsState, setCreditsChoice, type CreditsState } from './credits';
import styles from './tx.module.css';
import type { CheckoutQuote } from '@todayz/api-client/gen/model/checkoutQuote';

/**
 * C-09 / C-11 credits: a switch and an amount slider (0 to the most this cart can take). When a cap
 * was reached the switch is off and disabled with the reason (D-30, D-76); the order can still be
 * placed without credits. Money here is the server's: the total comes from the quote, not from this.
 */
export function CreditsControl({
  storeId,
  probe,
  quote,
  on,
  amount,
}: {
  storeId: string;
  probe: CheckoutQuote | undefined;
  /** The quote for what was chosen; `creditsUsed` is what the server will really take. */
  quote: CheckoutQuote | undefined;
  on: boolean;
  amount: number;
}) {
  const intl = useIntl();
  const state: CreditsState = creditsState(probe);
  if (state.kind === 'hidden') return null;

  if (state.kind === 'blocked') {
    return (
      <div className={styles.credits}>
        <label className={styles.creditsHead}>
          <input type="checkbox" role="switch" checked={false} disabled aria-describedby="credits-why" />
          <span className={styles.name}>{intl.formatMessage({ id: 'cart.credits.use' })}</span>
        </label>
        <p id="credits-why" className={styles.meta}>
          {intl.formatMessage({ id: `cart.credits.blocked.${state.limit}` })}
        </p>
      </div>
    );
  }

  const chosen = Math.min(amount, state.max);
  const used = on ? (quote?.creditsUsed ?? chosen) : 0;
  return (
    <div className={styles.credits}>
      <label className={styles.creditsHead}>
        <input
          type="checkbox"
          role="switch"
          checked={on}
          onChange={(e) => setCreditsChoice(storeId, e.target.checked, state.max)}
        />
        <span className={styles.name}>{intl.formatMessage({ id: 'cart.credits.use' })}</span>
      </label>
      {!on && <p className={styles.meta}>{intl.formatMessage({ id: 'cart.credits.hint' }, { max: state.max })}</p>}
      {on && (
        <>
          <input
            type="range"
            className={styles.creditsRange}
            min={0}
            max={state.max}
            step={1}
            value={chosen}
            aria-label={intl.formatMessage({ id: 'cart.credits.amount' })}
            aria-valuetext={intl.formatMessage({ id: 'cart.credits.valueText' }, { count: chosen, off: formatMoney(chosen * 100) })}
            onChange={(e) => setCreditsChoice(storeId, true, Number(e.target.value))}
          />
          <p className={styles.meta} aria-live="polite">
            {intl.formatMessage({ id: 'cart.credits.using' }, { count: used, off: formatMoney(used * 100) })}
          </p>
        </>
      )}
    </div>
  );
}
