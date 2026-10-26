// C-17 Scan bill, as opened from the phone's camera: `/scan/$token` is the POS bill QR (business
// `pos/links.ts`). Signed out, the person signs in first and comes back here. The preview names the
// shop, the amount and the most credits the bill allows; Confirm links the bill to this account and
// the counter sees it at once. The token is a 120 s bearer: it stays in the URL and this page's
// state only, never in storage or a log. The in-app camera scanner and the counter code are not
// built yet.
import type { BillClaimConfirmation } from '@todayz/api-client/gen/model/billClaimConfirmation';
import type { BillClaimPreview } from '@todayz/api-client/gen/model/billClaimPreview';
import { confirmBillClaim, previewBillClaim } from '@todayz/api-client/gen/pos/pos';
import { ApiError } from '@todayz/api-client';
import { SessionPending, useSessionState } from '@todayz/auth';
import { formatMoney } from '@todayz/i18n';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useRouter } from '@tanstack/react-router';
import { Banner, Button, EmptyState, Skeleton } from '@todayz/ui';
import { Check, Clock, QrCode, SearchX } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useIntl } from 'react-intl';
import styles from '../cart/tx.module.css';
import { RouterLink } from '../home/RouterLink';
import { formatMmSs, useServerCountdown } from '../orders/clock';
import { newUlid } from '../orders/ulid';
import { Icon } from '../shell/Icon';

type Failure = 'expired' | 'used' | 'notFound' | 'error';

function failureOf(err: unknown): Failure {
  if (err instanceof ApiError) {
    if (err.code === 'claim_expired') return 'expired';
    if (err.code === 'claim_already_used') return 'used';
    if (err.status === 404) return 'notFound';
  }
  return 'error';
}

export function ScanBill({ token }: { token: string }) {
  const intl = useIntl();
  const router = useRouter();
  const { status } = useSessionState();

  useEffect(() => {
    if (status === 'signedOut') router.history.replace(`/sign-in?next=${encodeURIComponent(`/scan/${token}`)}`);
  }, [status, token, router]);

  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'scan.title' })}</h1>
      {status === 'signedIn' ? (
        <Claim token={token} />
      ) : status === 'unknown' ? (
        <SessionPending
          loadingLabel={intl.formatMessage({ id: 'scan.title' })}
          offlineTitle={intl.formatMessage({ id: 'app.offline.title' })}
          offlineMessage={intl.formatMessage({ id: 'app.offline.body' })}
          retryLabel={intl.formatMessage({ id: 'app.pending.retry' })}
        />
      ) : (
        <Skeleton shape="block" height="160px" />
      )}
    </div>
  );
}

function Claim({ token }: { token: string }) {
  const preview = useQuery<BillClaimPreview, Error>({
    queryKey: ['bill-claim', token],
    queryFn: () => previewBillClaim(token),
    retry: false,
    gcTime: 0,
    refetchOnWindowFocus: false,
  });
  // One Idempotency-Key per confirm attempt; a retry after a dropped connection reuses it.
  const key = useRef<string | null>(null);
  const confirm = useMutation<BillClaimConfirmation, Error, number>({
    mutationFn: (creditsToUse) => {
      key.current ??= newUlid();
      return confirmBillClaim(token, { creditsToUse }, { idempotencyKey: key.current });
    },
    onSettled: (_data, err) => {
      if (!err || err instanceof ApiError) key.current = null;
    },
  });

  if (confirm.data) return <Done done={confirm.data} />;
  if (preview.isPending) return <Skeleton shape="block" height="160px" />;
  if (preview.isError) return <Failed failure={failureOf(preview.error)} retry={() => void preview.refetch()} />;
  const confirmFailure = confirm.error ? failureOf(confirm.error) : null;
  if (confirmFailure && confirmFailure !== 'error') return <Failed failure={confirmFailure} />;
  return <Preview bill={preview.data} sending={confirm.isPending} failed={confirmFailure === 'error'} onConfirm={(credits) => confirm.mutate(credits)} />;
}

function Preview({ bill, sending, failed, onConfirm }: { bill: BillClaimPreview; sending: boolean; failed: boolean; onConfirm: (credits: number) => void }) {
  const intl = useIntl();
  const left = useServerCountdown(bill.expiresAt, bill.serverNow);
  const [on, setOn] = useState(false);
  const [amount, setAmount] = useState(bill.maxCredits);
  if (left === 0) return <Failed failure="expired" />;
  const credits = on ? Math.min(amount, bill.maxCredits) : 0;
  const blocked = bill.maxCredits === 0 && (bill.creditsLimit === 'store_credit_cap' || bill.creditsLimit === 'credit_monthly_cap' || bill.creditsLimit === 'below_credit_minimum') ? bill.creditsLimit : null;

  return (
    <section className="tz-card" aria-label={intl.formatMessage({ id: 'scan.title' })}>
      <div className={styles.stack}>
        <h2 className="tz-card-title">{intl.formatMessage({ id: 'scan.at' }, { amount: formatMoney(bill.amountPaise, 'receipt'), shop: bill.store.name })}</h2>
        <p className={styles.meta}>{intl.formatMessage({ id: 'scan.help' })}</p>
        {left !== null && (
          <p className={styles.countdown} aria-live="off">
            <Icon icon={Clock} /> {intl.formatMessage({ id: 'scan.expiresIn' }, { left: formatMmSs(left) })}
          </p>
        )}
        {bill.maxCredits > 0 && (
          <div className={styles.credits}>
            <label className={styles.creditsHead}>
              <input type="checkbox" role="switch" checked={on} onChange={(e) => setOn(e.target.checked)} />
              <span className={styles.name}>{intl.formatMessage({ id: 'scan.credits.use' })}</span>
            </label>
            {!on && <p className={styles.meta}>{intl.formatMessage({ id: 'scan.credits.hint' }, { max: bill.maxCredits })}</p>}
            {on && (
              <>
                <input
                  type="range"
                  className={styles.creditsRange}
                  min={0}
                  max={bill.maxCredits}
                  step={1}
                  value={credits}
                  aria-label={intl.formatMessage({ id: 'scan.credits.amount' })}
                  aria-valuetext={intl.formatMessage({ id: 'scan.credits.valueText' }, { count: credits, off: formatMoney(credits * 100) })}
                  onChange={(e) => setAmount(Number(e.target.value))}
                />
                <p className={styles.meta} aria-live="polite">
                  {intl.formatMessage({ id: 'scan.credits.using' }, { count: credits, off: formatMoney(credits * 100) })}
                </p>
              </>
            )}
          </div>
        )}
        {blocked && (
          <div className={styles.credits}>
            <label className={styles.creditsHead}>
              <input type="checkbox" role="switch" checked={false} disabled aria-describedby="scan-credits-why" />
              <span className={styles.name}>{intl.formatMessage({ id: 'scan.credits.use' })}</span>
            </label>
            <p id="scan-credits-why" className={styles.meta}>
              {intl.formatMessage({ id: `scan.credits.blocked.${blocked}` })}
            </p>
          </div>
        )}
        {failed && (
          <Banner tone="warning" live="status">
            {intl.formatMessage({ id: 'scan.error' })}
          </Banner>
        )}
        <Button variant="primary" block loading={sending} disabled={sending} onClick={() => onConfirm(credits)}>
          {intl.formatMessage({ id: sending ? 'scan.confirming' : 'scan.confirm' })}
        </Button>
      </div>
    </section>
  );
}

function Done({ done }: { done: BillClaimConfirmation }) {
  const intl = useIntl();
  return (
    <section className="tz-card">
      <div className={styles.stack}>
        <div className={styles.hero}>
          <span className={styles.check}>
            <Icon icon={Check} />
          </span>
          <h2 className="tz-card-title">{intl.formatMessage({ id: 'scan.done.title' })}</h2>
        </div>
        <p className={styles.name}>{intl.formatMessage({ id: 'scan.done.pay' }, { amount: formatMoney(done.payablePaise, 'receipt') })}</p>
        {done.creditsUsed > 0 && <p className={styles.meta}>{intl.formatMessage({ id: 'scan.done.credits' }, { count: done.creditsUsed })}</p>}
        <RouterLink href="/orders" className="tz-btn tz-btn-soft">
          {intl.formatMessage({ id: 'scan.done.orders' })}
        </RouterLink>
      </div>
    </section>
  );
}

function Failed({ failure, retry }: { failure: Failure; retry?: () => void }) {
  const intl = useIntl();
  const router = useRouter();
  const title = { expired: 'scan.expired', used: 'scan.used', notFound: 'scan.notFound', error: 'scan.error' }[failure];
  return (
    <EmptyState
      icon={failure === 'error' ? SearchX : QrCode}
      title={intl.formatMessage({ id: title })}
      {...(failure === 'expired' ? { description: intl.formatMessage({ id: 'scan.expired.body' }) } : {})}
      action={
        failure === 'error' && retry ? (
          <Button variant="soft" onClick={retry}>
            {intl.formatMessage({ id: 'scan.retry' })}
          </Button>
        ) : (
          <Button variant="soft" onClick={() => router.history.push('/')}>
            {intl.formatMessage({ id: 'scan.home' })}
          </Button>
        )
      }
    />
  );
}
