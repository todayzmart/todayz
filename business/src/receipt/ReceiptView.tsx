// The counter receipt: what the customer gets on paper (80 mm) or as a link. It shows the shop, the
// lines and the totals the server finished with, and never a customer's name or number (C-24).
import { formatMoney } from '@todayz/i18n';
import { useIntl } from 'react-intl';
import type { Sale } from '../pos/types';
import styles from './receipt.module.css';

export interface ReceiptViewProps {
  shopName: string;
  sale: Sale;
}

export function ReceiptView({ shopName, sale }: ReceiptViewProps) {
  const intl = useIntl();
  const money = (p: number) => formatMoney(p, 'receipt');
  return (
    <article className={`${styles.paper} tz-num`} aria-label={intl.formatMessage({ id: 'pos.receipt.title' })}>
      <header className={styles.head}>
        <strong>{shopName}</strong>
        <span>{intl.formatDate(sale.soldAt, { dateStyle: 'medium', timeStyle: 'short', timeZone: 'Asia/Kolkata' })}</span>
      </header>
      <ul className={styles.lines}>
        {sale.lines.map((l) => (
          <li key={l.clientId}>
            <span className={styles.name}>{l.name}</span>
            <span className={styles.qty}>
              {l.quantity} × {money(l.unitPricePaise)}
            </span>
            <span className={styles.amt}>{money(l.lineTotalPaise)}</span>
            {l.discountPaise > 0 && (
              <span className={styles.note}>
                {intl.formatMessage({ id: 'pos.receipt.lineDiscount' })} −{money(l.discountPaise)}
              </span>
            )}
          </li>
        ))}
      </ul>
      <dl className={styles.totals}>
        <Row label={intl.formatMessage({ id: 'pos.receipt.subtotal' })} value={money(sale.subtotalPaise)} />
        {sale.billDiscountPaise > 0 && (
          <Row label={intl.formatMessage({ id: 'pos.receipt.discount' })} value={`−${money(sale.billDiscountPaise)}`} />
        )}
        {sale.roundOffPaise !== 0 && (
          <Row
            label={intl.formatMessage({ id: 'pos.receipt.roundOff' })}
            value={`${sale.roundOffPaise < 0 ? '−' : '+'}${money(Math.abs(sale.roundOffPaise))}`}
          />
        )}
        {sale.creditsUsed > 0 && (
          <Row label={intl.formatMessage({ id: 'pos.receipt.credits' })} value={`−${money(sale.creditsUsed * 100)}`} />
        )}
        <Row strong label={intl.formatMessage({ id: 'pos.receipt.total' })} value={money(sale.totalPaise)} />
        <Row label={intl.formatMessage({ id: 'pos.receipt.paidBy' })} value={intl.formatMessage({ id: `pos.pay.${sale.paymentMethod}` })} />
      </dl>
      <p className={styles.thanks}>{intl.formatMessage({ id: 'pos.receipt.thanks' })}</p>
    </article>
  );
}

function Row({ label, value, strong = false }: { label: string; value: string; strong?: boolean }) {
  return (
    <div className={strong ? styles.strong : undefined}>
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}
