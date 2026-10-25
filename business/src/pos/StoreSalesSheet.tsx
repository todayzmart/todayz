// The store's sales as the server holds them (B-05 sales history, W2-16): every device, online and
// synced, newest first, with each sale's customer link. An offline counter code the server could
// not verify shows "Customer link failed" and why (D-12). Read only and online only: the device's
// own outbox, with sync, retry and reverse, stays in SalesSheet.
import { ApiError } from '@todayz/api-client';
import { formatMoney } from '@todayz/i18n';
import { Banner, Button, EmptyState, Sheet, Skeleton, StatusPill } from '@todayz/ui';
import { RefreshCw } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useIntl } from 'react-intl';
import type { StoreSale } from './api';
import { usePos } from './context';
import styles from './pos.module.css';

export interface StoreSalesSheetProps {
  open: boolean;
  onClose: () => void;
}

const money = (p: number) => formatMoney(p, 'product');

export function StoreSalesSheet({ open, onClose }: StoreSalesSheetProps) {
  const intl = useIntl();
  const { api, online } = usePos();
  const [sales, setSales] = useState<StoreSale[] | null>(null);
  const [next, setNext] = useState<string | null>(null);
  /** What the buttons asked for; the first page on opening counts as loading while `sales` is null. */
  const [pending, setPending] = useState<'refresh' | 'more' | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  // Only the latest request may write: a refresh while "Load more" is in flight wins.
  const req = useRef(0);

  const fetchPage = useCallback(
    (cursor: string | null) => {
      const id = ++req.current;
      api.listSales(cursor).then(
        (page) => {
          if (id !== req.current) return;
          setSales((prev) => (cursor && prev ? [...prev, ...page.items] : page.items));
          setNext(page.nextCursor);
          setFailed(null);
          setPending(null);
        },
        (e: unknown) => {
          if (id !== req.current) return;
          setFailed(e instanceof ApiError ? e.code : 'network');
          setPending(null);
        },
      );
    },
    [api],
  );

  useEffect(() => {
    if (open && online) fetchPage(null);
    else req.current++;
  }, [open, online, fetchPage]);

  const ask = (cursor: string | null) => {
    setPending(cursor ? 'more' : 'refresh');
    fetchPage(cursor);
  };
  const loading = pending === 'refresh' || (sales === null && failed === null && online && open);

  const when = (iso: string) =>
    intl.formatDate(iso, { day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit', timeZone: 'Asia/Kolkata' });

  return (
    <Sheet open={open} onClose={onClose} title={intl.formatMessage({ id: 'pos.storeSales.title' })} closeLabel={intl.formatMessage({ id: 'pos.close' })}>
      <div className={styles.sheetBody}>
        <div className={styles.saleTop}>
          <p className="tz-sub">{intl.formatMessage({ id: 'pos.storeSales.sub' })}</p>
          <Button className={styles.tap} icon={RefreshCw} disabled={!online || loading} onClick={() => ask(null)}>
            {intl.formatMessage({ id: 'pos.storeSales.refresh' })}
          </Button>
        </div>

        {!online && <Banner tone="warning">{intl.formatMessage({ id: 'pos.storeSales.offline' })}</Banner>}
        {failed && (
          <Banner tone="warning" live="status">
            {intl.formatMessage({ id: failed === 'forbidden' ? 'pos.storeSales.forbidden' : 'pos.storeSales.failed' })}
          </Banner>
        )}

        {sales === null ? (
          loading && (
            <div role="status" aria-label={intl.formatMessage({ id: 'pos.storeSales.loading' })}>
              <Skeleton height="160px" shape="block" />
            </div>
          )
        ) : sales.length === 0 ? (
          <EmptyState title={intl.formatMessage({ id: 'pos.storeSales.empty.title' })} description={intl.formatMessage({ id: 'pos.storeSales.empty.body' })} />
        ) : (
          <ul className={styles.history} aria-label={intl.formatMessage({ id: 'pos.storeSales.title' })}>
            {sales.map((s) => (
              <li key={s.id} className={styles.sale}>
                <div className={styles.saleTop}>
                  <span>
                    <b className="tz-num">{money(s.totalPaise)}</b>{' '}
                    <span className="tz-sub">
                      {intl.formatMessage({ id: 'pos.history.units' }, { count: s.unitCount })} · {intl.formatMessage({ id: `pos.pay.${s.paymentMethod}` })}
                    </span>
                  </span>
                  <time className="tz-sub" dateTime={s.soldAt}>
                    {when(s.soldAt)}
                  </time>
                </div>
                <div className={styles.saleTags}>
                  {s.reversed && <StatusPill tone="cancelled">{intl.formatMessage({ id: 'pos.history.state.reversed' })}</StatusPill>}
                  {s.offline && <StatusPill>{intl.formatMessage({ id: 'pos.storeSales.offlineSale' })}</StatusPill>}
                  <LinkPill sale={s} />
                </div>
                {s.link?.status === 'failed' && s.link.failure && (
                  <ul className={styles.notes}>
                    <li>{intl.formatMessage({ id: `pos.storeSales.failure.${s.link.failure}` })}</li>
                  </ul>
                )}
              </li>
            ))}
          </ul>
        )}

        {next && sales && (
          <Button className={styles.tap} variant="soft" loading={pending === 'more'} disabled={!online || loading} onClick={() => ask(next)}>
            {intl.formatMessage({ id: 'pos.storeSales.more' })}
          </Button>
        )}
      </div>
    </Sheet>
  );
}

function LinkPill({ sale }: { sale: StoreSale }) {
  const intl = useIntl();
  const l = sale.link;
  if (!l) return <StatusPill>{intl.formatMessage({ id: 'pos.storeSales.link.none' })}</StatusPill>;
  if (l.status === 'failed') return <StatusPill tone="rejected">{intl.formatMessage({ id: 'pos.history.linkFailed' })}</StatusPill>;
  if (l.status === 'pending_verification') return <StatusPill tone="pending">{intl.formatMessage({ id: 'pos.storeSales.link.pending' })}</StatusPill>;
  return (
    <StatusPill tone="completed">
      {l.customerName ? intl.formatMessage({ id: 'pos.storeSales.link.linkedTo' }, { name: l.customerName }) : intl.formatMessage({ id: 'pos.storeSales.link.linked' })}
    </StatusPill>
  );
}
