// C-24 the public POS receipt, `/r/$token` (D-45): no sign-in, the shop, the items and the totals,
// never any customer data. Opened from the counter's WhatsApp/SMS share and from C-13 In store. An
// unknown or expired (30 days) token is the server's 404: "This receipt link has expired". The page
// asks search engines not to index it.
import type { Receipt } from '@todayz/api-client/gen/model/receipt';
import { getPublicReceipt } from '@todayz/api-client/gen/receipts/receipts';
import { ApiError } from '@todayz/api-client';
import { formatAbsolute, formatMoney } from '@todayz/i18n';
import { useQuery } from '@tanstack/react-query';
import { useRouter } from '@tanstack/react-router';
import { Banner, Button, EmptyState, Skeleton } from '@todayz/ui';
import { ReceiptText, SearchX } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useIntl } from 'react-intl';
import { useLocale } from '../app/locale';
import styles from '../cart/tx.module.css';

function useNoIndex(): void {
  useEffect(() => {
    const meta = document.createElement('meta');
    meta.name = 'robots';
    meta.content = 'noindex';
    document.head.appendChild(meta);
    return () => meta.remove();
  }, []);
}

export function ReceiptPage({ token }: { token: string }) {
  const intl = useIntl();
  const router = useRouter();
  useNoIndex();
  const q = useQuery<Receipt, Error>({ queryKey: ['receipt', token], queryFn: () => getPublicReceipt(token), retry: false });

  const home = (
    <Button variant="soft" onClick={() => router.history.push('/')}>
      {intl.formatMessage({ id: 'receipt.home' })}
    </Button>
  );
  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'receipt.title' })}</h1>
      {q.isPending && <Skeleton shape="block" height="240px" />}
      {q.isError &&
        (q.error instanceof ApiError && q.error.status === 404 ? (
          <EmptyState icon={ReceiptText} title={intl.formatMessage({ id: 'receipt.expired' })} description={intl.formatMessage({ id: 'receipt.expired.body' })} action={home} />
        ) : (
          <EmptyState
            icon={SearchX}
            title={intl.formatMessage({ id: 'receipt.error' })}
            action={
              <Button variant="soft" onClick={() => void q.refetch()}>
                {intl.formatMessage({ id: 'receipt.retry' })}
              </Button>
            }
          />
        ))}
      {q.data && <ReceiptCard receipt={q.data} />}
    </div>
  );
}

function ReceiptCard({ receipt: r }: { receipt: Receipt }) {
  const intl = useIntl();
  const { locale } = useLocale();
  // Read once per visit: it only picks "Today" or "Yesterday" for the sale time.
  const [now] = useState(Date.now);
  const money = (paise: number) => formatMoney(paise, 'receipt');
  const address = [r.store.addressLine, r.store.locality].filter(Boolean).join(', ');
  return (
    <section className="tz-card" aria-label={r.store.name}>
      <div className={styles.stack}>
        <div>
          <h2 className="tz-card-title">{r.store.name}</h2>
          {address && <p className={styles.meta}>{address}</p>}
          <p className={styles.meta}>{formatAbsolute(r.soldAt, now, locale)}</p>
        </div>
        {r.status === 'reversed' && (
          <Banner tone="warning">
            {intl.formatMessage({ id: 'receipt.reversed' })}
          </Banner>
        )}
        <ul className={styles.list} aria-label={intl.formatMessage({ id: 'receipt.items' })}>
          {r.lines.map((l, i) => (
            <li key={i} className={styles.line}>
              <div>
                <p className={styles.name}>{l.name}</p>
                <p className={styles.meta}>
                  {intl.formatMessage({ id: 'receipt.qty' }, { count: String(l.quantity), price: money(l.unitPricePaise) })}
                  {l.discountPaise > 0 && ` · ${intl.formatMessage({ id: 'receipt.lineDiscount' }, { amount: money(l.discountPaise) })}`}
                </p>
              </div>
              <span className={styles.price}>{money(l.lineTotalPaise)}</span>
            </li>
          ))}
        </ul>
        <dl className={styles.totals}>
          <div className={styles.totalRow}>
            <dt>{intl.formatMessage({ id: 'receipt.subtotal' })}</dt>
            <dd>{money(r.itemSubtotalPaise)}</dd>
          </div>
          {r.discountPaise > 0 && (
            <div className={styles.totalRow}>
              <dt>{intl.formatMessage({ id: 'receipt.discount' })}</dt>
              <dd>−{money(r.discountPaise)}</dd>
            </div>
          )}
          {r.creditsUsed > 0 && (
            <div className={styles.totalRow}>
              <dt>{intl.formatMessage({ id: 'receipt.credits' })}</dt>
              <dd>−{money(r.creditsUsed * 100)}</dd>
            </div>
          )}
          {r.roundOffPaise !== 0 && (
            <div className={styles.totalRow}>
              <dt>{intl.formatMessage({ id: 'receipt.roundOff' })}</dt>
              <dd>{r.roundOffPaise < 0 ? `−${money(-r.roundOffPaise)}` : money(r.roundOffPaise)}</dd>
            </div>
          )}
          <div className={`${styles.totalRow} ${styles.grand}`}>
            <dt>{intl.formatMessage({ id: 'receipt.total' })}</dt>
            <dd>{money(r.totalPaise)}</dd>
          </div>
        </dl>
        <p className={styles.meta}>{intl.formatMessage({ id: 'receipt.paidBy' }, { method: r.paymentMethod })}</p>
      </div>
    </section>
  );
}
