import type { BusinessStatement } from '@todayz/api-client/gen/model/businessStatement';
import { formatMoney } from '@todayz/i18n';
import { Banner, Button, Skeleton, StatusPill, useToast, type PillTone } from '@todayz/ui';
import { Download, FileText } from 'lucide-react';
import { Fragment, useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { getStatementCsv, useStatements } from './queries';
import styles from './statements.module.css';

const money = (p: number) => formatMoney(p, 'product');

/** A period ("2026-10") as "October 2026". Mid-month in IST, so no time zone moves it to the next month. */
const monthDate = (period: string) => `${period}-15T12:00:00+05:30`;

const PAYOUT = {
  processing: { pill: 'statements.status.processing', line: 'statements.payout.processing', tone: 'pending' },
  needs_kyc: { pill: 'statements.status.needsKyc', line: 'statements.payout.needsKyc', tone: 'cancelled' },
  paid: { pill: 'statements.status.paid', line: 'statements.payout.paid', tone: 'completed' },
  carried: { pill: 'statements.status.carried', line: 'statements.payout.carried', tone: 'neutral' },
} as const satisfies Record<string, { pill: string; line: string; tone: PillTone }>;

/**
 * B-10 monthly statements (01 §9): each closed IST month with its closing balance, the payout's state
 * (processing, on hold for payout details, paid with the UTR, or carried into the next statement's
 * payout when a later close recomputed it), the breakdown and the CSV. Owner and
 * manager; a member limited to some stores gets only those stores' totals from the server.
 */
export function StatementsCard({ bid }: { bid: string }) {
  const intl = useIntl();
  const statements = useStatements(bid);
  const items = useMemo(() => statements.data?.pages.flatMap((p) => p.items) ?? [], [statements.data]);

  return (
    <section className="tz-card" aria-labelledby="st-title">
      <div className="tz-card-head">
        <h2 className="tz-card-title" id="st-title">
          {intl.formatMessage({ id: 'credits.statements.title' })}
        </h2>
      </div>
      {statements.isPending && (
        <div role="status" aria-label={intl.formatMessage({ id: 'statements.loading' })}>
          <Skeleton height="6rem" />
        </div>
      )}
      {statements.isError && !statements.data && (
        <Banner tone="warning" action={<Button loading={statements.isFetching} onClick={() => void statements.refetch()}>{intl.formatMessage({ id: 'statements.retry' })}</Button>}>
          {intl.formatMessage({ id: 'statements.error' })}
        </Banner>
      )}
      {statements.data && items.length === 0 && (
        <div className={styles.empty}>
          <FileText size={20} aria-hidden="true" />
          <p>{intl.formatMessage({ id: 'statements.empty' })}</p>
        </div>
      )}
      {items.length > 0 && (
        <div className={styles.stack}>
          <ul className={styles.list} aria-label={intl.formatMessage({ id: 'statements.list' })}>
            {items.map((s) => (
              <StatementItem key={s.period} bid={bid} statement={s} />
            ))}
          </ul>
          {statements.hasNextPage && (
            <div className={styles.more}>
              <Button
                loading={statements.isFetchingNextPage}
                loadingLabel={intl.formatMessage({ id: 'statements.loading' })}
                onClick={() => void statements.fetchNextPage()}
              >
                {intl.formatMessage({ id: 'statements.more' })}
              </Button>
            </div>
          )}
          {statements.isError && statements.data && (
            <p role="alert" className="tz-field-error">
              {intl.formatMessage({ id: 'statements.moreError' })}
            </p>
          )}
        </div>
      )}
    </section>
  );
}

function StatementItem({ bid, statement: s }: { bid: string; statement: BusinessStatement }) {
  const intl = useIntl();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const month = intl.formatDate(monthDate(s.period), { month: 'long', year: 'numeric', timeZone: 'Asia/Kolkata' });
  const t = s.totals;
  const payout = s.payout ? PAYOUT[s.payout.status] : null;

  const download = async () => {
    setBusy(true);
    try {
      const csv = await getStatementCsv(bid, s.period);
      const url = URL.createObjectURL(new Blob([csv], { type: 'text/csv' }));
      const a = document.createElement('a');
      a.href = url;
      a.download = `todayz-statement-${s.period}.csv`;
      a.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch {
      toast.show({ message: intl.formatMessage({ id: 'statements.downloadError' }) });
    } finally {
      setBusy(false);
    }
  };

  // cashback out and payouts out are what left the stores (positive); shown taken away.
  const rows = [
    ['statements.totals.opening', t.openingPaise],
    ['statements.totals.creditsIn', t.creditsInPaise],
    ['statements.totals.referralIn', t.referralInPaise],
    ['statements.totals.cashbackOut', -t.cashbackOutPaise],
    ['statements.totals.reversals', t.reversalsPaise],
    ['statements.totals.adjustments', t.adjustmentsPaise],
    ['statements.totals.payoutsOut', -t.payoutsOutPaise],
  ] as const;

  return (
    <li className={styles.item} data-testid={`statement-${s.period}`}>
      <div className={styles.head}>
        <span className={styles.month}>{month}</span>
        <span className={styles.closing}>{intl.formatMessage({ id: 'statements.closing' }, { amount: money(t.closingPaise) })}</span>
      </div>
      {s.payout && payout ? (
        <p className={styles.payout}>
          <StatusPill tone={payout.tone}>{intl.formatMessage({ id: payout.pill })}</StatusPill>
          <span>
            {intl.formatMessage(
              { id: payout.line },
              {
                amount: money(s.payout.amountPaise),
                date: s.payout.paidAt ? intl.formatDate(s.payout.paidAt, { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'Asia/Kolkata' }) : '',
              },
            )}
          </span>
          {s.payout.utr && <span className={styles.sub}>{intl.formatMessage({ id: 'statements.payout.utr' }, { utr: s.payout.utr })}</span>}
        </p>
      ) : (
        <p className={styles.sub}>{intl.formatMessage({ id: 'statements.payout.none' })}</p>
      )}
      <details className={styles.totals}>
        <summary>{intl.formatMessage({ id: 'statements.totals.title' })}</summary>
        <dl>
          {rows.map(([id, paise]) => (
            <Fragment key={id}>
              <dt>{intl.formatMessage({ id })}</dt>
              <dd className="tz-num">{money(paise)}</dd>
            </Fragment>
          ))}
          <dt className={styles.total}>{intl.formatMessage({ id: 'statements.totals.closing' })}</dt>
          <dd className={`tz-num ${styles.total}`}>{money(t.closingPaise)}</dd>
        </dl>
      </details>
      {t.closingPaise < 0 && <p className={styles.sub}>{intl.formatMessage({ id: 'statements.negative' })}</p>}
      <div className={styles.actions}>
        <Button
          icon={Download}
          loading={busy}
          loadingLabel={intl.formatMessage({ id: 'statements.downloading' })}
          aria-label={intl.formatMessage({ id: 'statements.downloadFor' }, { month })}
          onClick={() => void download()}
        >
          {intl.formatMessage({ id: 'statements.download' })}
        </Button>
      </div>
    </li>
  );
}
