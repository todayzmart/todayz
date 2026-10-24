// Sales on this device: the sync panel (what is waiting, what the server refused) and the history
// with receipt, reverse (manager and above, inside the window, D-08) and the conflicts the server
// found. Sales always apply; stock may go below 0 and is flagged "count needed" (02 §4.3).
import { ApiError } from '@todayz/api-client';
import { formatMoney } from '@todayz/i18n';
import { Banner, Button, Dialog, EmptyState, Sheet, StatusPill, TextField } from '@todayz/ui';
import { ReceiptText, RefreshCw, Undo2 } from 'lucide-react';
import { useRef, useState } from 'react';
import { useIntl } from 'react-intl';
import type { ReverseAck } from './api';
import { canReverse, needsAttention } from './history';
import { usePos } from './context';
import type { SaleRecord } from './types';
import styles from './pos.module.css';

export interface SalesSheetProps {
  open: boolean;
  onClose: () => void;
  /** Manager and above may reverse. */
  canReverseSales: boolean;
  onReceipt: (rec: SaleRecord) => void;
}

const money = (p: number) => formatMoney(p, 'product');

export function SalesSheet({ open, onClose, canReverseSales, onReceipt }: SalesSheetProps) {
  const intl = useIntl();
  const { records, sync, engine, online, settings, now, items } = usePos();
  const [confirm, setConfirm] = useState<SaleRecord | null>(null);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState<{ ack: ReverseAck } | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  const [reason, setReason] = useState('');
  const safe = useRef<HTMLButtonElement>(null);
  const attention = records.filter(needsAttention).length;

  const nameOf = (id: string) => items.find((i) => i.id === id)?.name ?? id;

  const reverse = async () => {
    if (!confirm || busy || reason.trim() === '') return;
    setBusy(true);
    setFailed(null);
    try {
      const ack = await engine.reverse(confirm.sale.clientId, reason.trim().slice(0, 200));
      setDone({ ack });
      setConfirm(null);
      setReason('');
    } catch (e) {
      setFailed(e instanceof ApiError ? e.code : 'network');
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <Sheet open={open} onClose={onClose} title={intl.formatMessage({ id: 'pos.history.title' })} closeLabel={intl.formatMessage({ id: 'pos.close' })}>
        <div className={styles.sheetBody}>
          <section aria-label={intl.formatMessage({ id: 'pos.sync.title' })}>
            <div className={styles.saleTop}>
              <b>{intl.formatMessage({ id: `pos.sync.status.${online ? sync.status : 'offline'}` })}</b>
              <Button size="sm" icon={RefreshCw} disabled={!online} onClick={() => void engine.flush({ force: true })}>
                {intl.formatMessage({ id: 'pos.sync.now' })}
              </Button>
            </div>
            <p className="tz-sub">
              {intl.formatMessage({ id: 'pos.sync.counts' }, { pending: sync.pending, rejected: sync.rejected })}
              {sync.lastSyncedAt && ` · ${intl.formatMessage({ id: 'pos.sync.last' }, { time: intl.formatTime(sync.lastSyncedAt, { hour: 'numeric', minute: '2-digit', timeZone: 'Asia/Kolkata' }) })}`}
            </p>
            {attention > 0 && <Banner tone="warning">{intl.formatMessage({ id: 'pos.sync.attention' }, { count: attention })}</Banner>}
          </section>

          {done && (
            <Banner live="status" action={<Button size="sm" variant="ghost" onClick={() => setDone(null)}>{intl.formatMessage({ id: 'pos.close' })}</Button>}>
              {intl.formatMessage(
                { id: 'pos.reverse.done' },
                { units: done.ack.stockReturned.reduce((n, s) => n + s.quantity, 0), credits: done.ack.creditsReturned, cashback: done.ack.cashbackClawedBack },
              )}
            </Banner>
          )}

          {records.length === 0 ? (
            <EmptyState title={intl.formatMessage({ id: 'pos.history.empty.title' })} description={intl.formatMessage({ id: 'pos.history.empty.body' })} />
          ) : (
            <ul className={styles.history}>
              {records.map((rec) => {
                const reversible = canReverseSales && canReverse(rec, settings.reversalWindowHours, now());
                const units = rec.sale.lines.reduce((n, l) => n + l.quantity, 0);
                return (
                  <li key={rec.sale.clientId} className={styles.sale}>
                    <div className={styles.saleTop}>
                      <span>
                        <b className="tz-num">{money(rec.serverTotalPaise ?? rec.sale.totalPaise)}</b>{' '}
                        <span className="tz-sub">
                          {intl.formatMessage({ id: 'pos.history.units' }, { count: units })} · {intl.formatMessage({ id: `pos.pay.${rec.sale.paymentMethod}` })} ·{' '}
                          {intl.formatTime(rec.sale.soldAt, { hour: 'numeric', minute: '2-digit', timeZone: 'Asia/Kolkata' })}
                        </span>
                      </span>
                    </div>
                    <div className={styles.saleTags}>
                      <StatusPill tone={rec.state === 'synced' ? 'completed' : rec.state === 'rejected' ? 'rejected' : 'neutral'}>
                        {intl.formatMessage({ id: `pos.history.state.${rec.reversed ? 'reversed' : rec.state}` })}
                      </StatusPill>
                      {rec.linkStatus === 'failed' && <StatusPill tone="pending">{intl.formatMessage({ id: 'pos.history.linkFailed' })}</StatusPill>}
                      {rec.stockFlags.length > 0 && <StatusPill tone="pending">{intl.formatMessage({ id: 'pos.history.countNeeded' })}</StatusPill>}
                    </div>
                    {(rec.stockFlags.length > 0 || (rec.serverTotalPaise !== null && rec.serverTotalPaise !== undefined && rec.serverTotalPaise !== rec.sale.totalPaise) || rec.state === 'rejected') && (
                      <ul className={styles.notes}>
                        {rec.stockFlags.length > 0 && <li>{intl.formatMessage({ id: 'pos.history.countNeededItems' }, { names: rec.stockFlags.map(nameOf).join(', ') })}</li>}
                        {rec.serverTotalPaise !== null && rec.serverTotalPaise !== undefined && rec.serverTotalPaise !== rec.sale.totalPaise && (
                          <li>{intl.formatMessage({ id: 'pos.history.serverPrice' }, { device: money(rec.sale.totalPaise), server: money(rec.serverTotalPaise) })}</li>
                        )}
                        {rec.state === 'rejected' && <li>{intl.formatMessage({ id: 'pos.history.refused' })}</li>}
                      </ul>
                    )}
                    <div className={styles.saleActions}>
                      <Button size="sm" icon={ReceiptText} onClick={() => onReceipt(rec)}>
                        {intl.formatMessage({ id: 'pos.history.receipt' })}
                      </Button>
                      {reversible && (
                        <Button size="sm" variant="soft-danger" icon={Undo2} disabled={!online} onClick={() => { setReason(''); setFailed(null); setConfirm(rec); }}>
                          {intl.formatMessage({ id: 'pos.reverse.action' })}
                        </Button>
                      )}
                      {rec.state === 'rejected' && (
                        <>
                          <Button size="sm" onClick={() => void engine.retry(rec.sale.clientId)}>
                            {intl.formatMessage({ id: 'pos.sync.retry' })}
                          </Button>
                          <Button size="sm" variant="ghost" onClick={() => void engine.dismiss(rec.sale.clientId)}>
                            {intl.formatMessage({ id: 'pos.sync.dismiss' })}
                          </Button>
                        </>
                      )}
                    </div>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </Sheet>

      <Dialog
        open={confirm !== null}
        role="alertdialog"
        dismissible={!busy}
        onClose={() => setConfirm(null)}
        initialFocusRef={safe}
        title={intl.formatMessage({ id: 'pos.reverse.title' })}
        description={intl.formatMessage({ id: 'pos.reverse.body' })}
        footer={
          <>
            <Button ref={safe} onClick={() => setConfirm(null)} disabled={busy}>
              {intl.formatMessage({ id: 'pos.reverse.keep' })}
            </Button>
            <Button variant="danger" loading={busy} disabled={reason.trim() === ''} onClick={() => void reverse()}>
              {intl.formatMessage({ id: 'pos.reverse.confirm' })}
            </Button>
          </>
        }
      >
        {confirm && (
          <>
            <ul className={styles.notes}>
              <li>{intl.formatMessage({ id: 'pos.reverse.stock' }, { count: confirm.sale.lines.reduce((n, l) => (l.storeItemId ? n + l.quantity : n), 0) })}</li>
              <li>{intl.formatMessage({ id: 'pos.reverse.credits' }, { count: confirm.sale.creditsUsed })}</li>
              <li>{intl.formatMessage({ id: 'pos.reverse.cashback' }, { count: confirm.cashbackCredits ?? 0 })}</li>
            </ul>
            <TextField
              label={intl.formatMessage({ id: 'pos.reverse.reason' })}
              value={reason}
              maxLength={200}
              onChange={(e) => setReason(e.target.value)}
            />
            {failed && (
              <Banner tone="warning" live="status">
                {intl.formatMessage({ id: failed === 'network' ? 'pos.reverse.offline' : failed === 'reversal_window_passed' ? 'pos.reverse.windowPassed' : 'pos.reverse.failed' })}
              </Banner>
            )}
          </>
        )}
      </Dialog>
    </>
  );
}
