// One counter sale (D-268): a side panel on wide screens, a bottom sheet on phones. Lines, discounts,
// payment, credits, cashier and device, the times, the customer link (with why a counter code failed,
// D-12) and the reversal. Reverse is manager and above inside `pos_reversal_window_hours` (the server
// says until when); the receipt opens only from the device that holds its token.
import { ApiError } from '@todayz/api-client';
import type { PosSaleDetail } from '@todayz/api-client/gen/model/posSaleDetail';
import type { PosSaleLink } from '@todayz/api-client/gen/model/posSaleLink';
import { formatMoney } from '@todayz/i18n';
import { Banner, Button, Dialog, Sheet, Skeleton, StatusPill, TextField, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { Copy, ReceiptText, Undo2 } from 'lucide-react';
import { useRef, useState } from 'react';
import { useIntl } from 'react-intl';
import { usePosMaybe } from '../pos/context';
import { ReceiptSheet } from '../receipt/ReceiptSheet';
import { useNow } from './queries';
import { canReverseSale, reverseSale, salesKey, shortId, useSale } from './salesQueries';
import type { CurrentStore } from './store';
import styles from './sales.module.css';

const money = (p: number) => formatMoney(p, 'product');

export function LinkPill({ link }: { link: PosSaleLink | null }) {
  const intl = useIntl();
  if (!link) return <StatusPill>{intl.formatMessage({ id: 'pos.storeSales.link.none' })}</StatusPill>;
  if (link.status === 'failed') return <StatusPill tone="rejected">{intl.formatMessage({ id: 'pos.history.linkFailed' })}</StatusPill>;
  if (link.status === 'pending_verification') return <StatusPill tone="pending">{intl.formatMessage({ id: 'pos.storeSales.link.pending' })}</StatusPill>;
  return (
    <StatusPill tone="completed">
      {link.customerName ? intl.formatMessage({ id: 'pos.storeSales.link.linkedTo' }, { name: link.customerName }) : intl.formatMessage({ id: 'pos.storeSales.link.linked' })}
    </StatusPill>
  );
}

/** Why an offline counter code did not link (D-12); nothing for any other link. */
export function LinkWhy({ link }: { link: PosSaleLink | null }) {
  const intl = useIntl();
  if (link?.status !== 'failed' || !link.failure) return null;
  return <span className={styles.why}>{intl.formatMessage({ id: `pos.storeSales.failure.${link.failure}` })}</span>;
}

export interface SaleDetailProps {
  store: CurrentStore;
  saleId: string | undefined;
  onClose: () => void;
}

export function SaleDetail({ store, saleId, onClose }: SaleDetailProps) {
  const intl = useIntl();
  const q = useSale(store, saleId);
  const title = saleId ? intl.formatMessage({ id: 'orders.sales.detail.title' }, { id: shortId(saleId) }) : '';
  return (
    <Sheet open={saleId !== undefined} onClose={onClose} title={title} closeLabel={intl.formatMessage({ id: 'pos.close' })} className={styles.panel}>
      <div className={styles.panelBody}>
        {q.isError ? (
          <Banner tone="warning">
            {intl.formatMessage({ id: q.error instanceof ApiError && q.error.status === 404 ? 'orders.sales.detail.notFound' : 'orders.sales.detail.error' })}
          </Banner>
        ) : !q.data ? (
          <div role="status" aria-label={intl.formatMessage({ id: 'orders.sales.loading' })}>
            <Skeleton height="16rem" shape="block" />
          </div>
        ) : (
          <SaleBody store={store} sale={q.data} fetchedAt={q.dataUpdatedAt} />
        )}
      </div>
    </Sheet>
  );
}

function SaleBody({ store, sale, fetchedAt }: { store: CurrentStore; sale: PosSaleDetail; fetchedAt: number }) {
  const intl = useIntl();
  const toast = useToast();
  const pos = usePosMaybe();
  const qc = useQueryClient();
  const [receiptOpen, setReceiptOpen] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<string | null>(null);
  const safe = useRef<HTMLButtonElement>(null);
  const now = useNow();

  const when = (iso: string) =>
    intl.formatDate(iso, { day: 'numeric', month: 'short', year: 'numeric', hour: 'numeric', minute: '2-digit', timeZone: 'Asia/Kolkata' });
  const person = (p: { name: string | null } | null) => p?.name ?? intl.formatMessage({ id: 'orders.sales.formerStaff' });
  const local = pos?.records.find((r) => r.saleId === sale.id) ?? null;
  const receipt = local?.receiptToken ? local : null;
  // The window is the server's: compare with its clock, not the device's.
  const serverNow = now + (Date.parse(sale.serverNow) - fetchedAt);
  const reversible =
    canReverseSale(store.role) && sale.status === 'completed' && sale.reversibleUntil !== null && Date.parse(sale.reversibleUntil) > serverNow;
  const billDiscount = sale.billOfferDiscountPaise + sale.discountPaise;
  const units = sale.lines.reduce((n, l) => (l.storeItemId ? n + l.quantity : n), 0);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(sale.id);
      toast.show({ message: intl.formatMessage({ id: 'orders.sales.detail.copied' }) });
    } catch {
      toast.show({ message: sale.id });
    }
  };

  const reverse = async () => {
    const why = reason.trim().slice(0, 200);
    if (busy || why === '') return;
    setBusy(true);
    setFailed(null);
    try {
      // The key is fixed per sale, as on the Sell screen, so the two ways in cannot reverse it twice.
      if (local) await pos?.engine.reverse(local.sale.clientId, why);
      else await reverseSale(store, sale.id, why, `reverse-${sale.id}`);
      setConfirm(false);
      setReason('');
      toast.show({ message: intl.formatMessage({ id: 'orders.sales.detail.reversed' }) });
      await qc.invalidateQueries({ queryKey: salesKey(store.sid) });
    } catch (e) {
      setFailed(e instanceof ApiError ? e.code : 'network');
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <div className={styles.cardTop}>
        <b className={`tz-num ${styles.total}`}>{money(sale.totalPaise)}</b>
        <span className={styles.tags}>
          {sale.status === 'reversed' ? (
            <StatusPill tone="cancelled">{intl.formatMessage({ id: 'pos.history.state.reversed' })}</StatusPill>
          ) : (
            <StatusPill tone="completed">{intl.formatMessage({ id: 'orders.sales.status.completed' })}</StatusPill>
          )}
          {sale.offline && <StatusPill>{intl.formatMessage({ id: 'pos.storeSales.offlineSale' })}</StatusPill>}
        </span>
      </div>

      {sale.status === 'reversed' && (
        <section className={styles.reversed} aria-label={intl.formatMessage({ id: 'orders.sales.detail.reversal' })}>
          <h3 className={styles.h3}>{intl.formatMessage({ id: 'orders.sales.detail.reversal' })}</h3>
          <dl className={styles.facts}>
            <dt>{intl.formatMessage({ id: 'orders.sales.detail.reason' })}</dt>
            <dd>{sale.reversalReason ?? '—'}</dd>
            <dt>{intl.formatMessage({ id: 'orders.sales.detail.by' })}</dt>
            <dd>{person(sale.reversedBy)}</dd>
            <dt>{intl.formatMessage({ id: 'orders.sales.detail.when' })}</dt>
            <dd>{sale.reversedAt ? <time dateTime={sale.reversedAt}>{when(sale.reversedAt)}</time> : '—'}</dd>
          </dl>
        </section>
      )}

      <section aria-label={intl.formatMessage({ id: 'orders.sales.detail.items' })}>
        <h3 className={styles.h3}>{intl.formatMessage({ id: 'orders.sales.detail.items' })}</h3>
        <ul className={styles.lines}>
          {sale.lines.map((l) => {
            const off = l.offerDiscountPaise + l.manualDiscountPaise;
            return (
              <li key={l.id} className={styles.line}>
                <span>
                  {l.name}
                  <span className="tz-sub">
                    {' '}
                    {intl.formatMessage({ id: 'orders.sales.detail.qty' }, { qty: l.quantity, price: money(l.unitPricePaise) })}
                    {off > 0 && ` · ${intl.formatMessage({ id: 'orders.sales.detail.lineDiscount' }, { amount: money(off) })}`}
                  </span>
                </span>
                <b className="tz-num">{money(l.lineTotalPaise)}</b>
              </li>
            );
          })}
        </ul>
        <dl className={styles.sums}>
          <dt>{intl.formatMessage({ id: 'pos.receipt.subtotal' })}</dt>
          <dd className="tz-num">{money(sale.itemSubtotalPaise)}</dd>
          {billDiscount > 0 && (
            <>
              <dt>{intl.formatMessage({ id: 'orders.sales.detail.billDiscount' })}</dt>
              <dd className="tz-num">−{money(billDiscount)}</dd>
            </>
          )}
          {sale.roundOffPaise !== 0 && (
            <>
              <dt>{intl.formatMessage({ id: 'pos.receipt.roundOff' })}</dt>
              <dd className="tz-num">{money(sale.roundOffPaise)}</dd>
            </>
          )}
          <dt>
            <b>{intl.formatMessage({ id: 'pos.receipt.total' })}</b>
          </dt>
          <dd className="tz-num">
            <b>{money(sale.totalPaise)}</b>
          </dd>
        </dl>
      </section>

      <dl className={styles.facts}>
        <dt>{intl.formatMessage({ id: 'orders.sales.col.payment' })}</dt>
        <dd>{intl.formatMessage({ id: `pos.pay.${sale.paymentMethod}` })}</dd>
        <dt>{intl.formatMessage({ id: 'orders.sales.kpi.credits' })}</dt>
        <dd className="tz-num">{sale.creditsUsed}</dd>
        <dt>{intl.formatMessage({ id: 'orders.sales.detail.cashback' })}</dt>
        <dd className="tz-num">{sale.cashbackCredits}</dd>
        <dt>{intl.formatMessage({ id: 'orders.sales.col.cashier' })}</dt>
        <dd>{person(sale.cashier)}</dd>
        <dt>{intl.formatMessage({ id: 'orders.sales.detail.device' })}</dt>
        <dd className="tz-num">#{sale.deviceId.slice(-6)}</dd>
        <dt>{intl.formatMessage({ id: 'orders.sales.detail.soldAt' })}</dt>
        <dd>
          <time dateTime={sale.soldAt}>{when(sale.soldAt)}</time>
        </dd>
        <dt>{intl.formatMessage({ id: 'orders.sales.detail.receivedAt' })}</dt>
        <dd>
          <time dateTime={sale.receivedAt}>{when(sale.receivedAt)}</time>
        </dd>
        <dt>{intl.formatMessage({ id: 'orders.sales.col.link' })}</dt>
        <dd>
          <LinkPill link={sale.link} />
          <LinkWhy link={sale.link} />
        </dd>
        <dt>{intl.formatMessage({ id: 'orders.sales.detail.billId' })}</dt>
        <dd className={styles.billId}>{sale.id}</dd>
      </dl>

      <div className={styles.actions}>
        <Button size="sm" icon={Copy} onClick={() => void copy()}>
          {intl.formatMessage({ id: 'orders.sales.detail.copy' })}
        </Button>
        {receipt && (
          <Button size="sm" icon={ReceiptText} onClick={() => setReceiptOpen(true)}>
            {intl.formatMessage({ id: 'pos.history.receipt' })}
          </Button>
        )}
        {reversible && (
          <Button size="sm" variant="soft-danger" icon={Undo2} onClick={() => { setReason(''); setFailed(null); setConfirm(true); }}>
            {intl.formatMessage({ id: 'pos.reverse.action' })}
          </Button>
        )}
      </div>
      {!receipt && <p className="tz-sub">{intl.formatMessage({ id: 'orders.sales.detail.receiptElsewhere' })}</p>}
      {reversible && sale.reversibleUntil && (
        <p className="tz-sub">{intl.formatMessage({ id: 'orders.sales.detail.reversibleUntil' }, { time: when(sale.reversibleUntil) })}</p>
      )}

      <ReceiptSheet open={receiptOpen} onClose={() => setReceiptOpen(false)} shopName={store.name} record={receipt} />

      <Dialog
        open={confirm}
        role="alertdialog"
        dismissible={!busy}
        onClose={() => setConfirm(false)}
        initialFocusRef={safe}
        title={intl.formatMessage({ id: 'pos.reverse.title' })}
        description={intl.formatMessage({ id: 'pos.reverse.body' })}
        footer={
          <>
            <Button ref={safe} onClick={() => setConfirm(false)} disabled={busy}>
              {intl.formatMessage({ id: 'pos.reverse.keep' })}
            </Button>
            <Button variant="danger" loading={busy} disabled={reason.trim() === ''} onClick={() => void reverse()}>
              {intl.formatMessage({ id: 'pos.reverse.confirm' })}
            </Button>
          </>
        }
      >
        <ul className={styles.notes}>
          <li>{intl.formatMessage({ id: 'pos.reverse.stock' }, { count: units })}</li>
          <li>{intl.formatMessage({ id: 'pos.reverse.credits' }, { count: sale.creditsUsed })}</li>
          <li>{intl.formatMessage({ id: 'pos.reverse.cashback' }, { count: sale.cashbackCredits })}</li>
        </ul>
        <TextField label={intl.formatMessage({ id: 'pos.reverse.reason' })} value={reason} maxLength={200} onChange={(e) => setReason(e.target.value)} />
        {failed && (
          <Banner tone="warning" live="status">
            {intl.formatMessage({ id: failed === 'network' ? 'pos.reverse.offline' : failed === 'reversal_window_passed' ? 'pos.reverse.windowPassed' : 'pos.reverse.failed' })}
          </Banner>
        )}
      </Dialog>
    </>
  );
}
