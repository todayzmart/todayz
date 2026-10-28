import { Banner, Button, EmptyState, Skeleton } from '@todayz/ui';
import { Inbox, Lock, Store as StoreIcon } from 'lucide-react';
import { Link } from '@tanstack/react-router';
import { useMemo } from 'react';
import { useIntl } from 'react-intl';
import { useCurrentStore, type CurrentStore } from '../orders/store';
import { CreditLines } from './CreditLines';
import styles from './credits.module.css';
import { PayoutsCard } from './PayoutsCard';
import { canSeeCredits } from './permissions';
import { useCreditAccount, useReferrals } from './queries';
import { ReferralCard } from './ReferralCard';
import { StandeeCard } from './StandeeCard';
import { StatementsCard } from '../statements/StatementsCard';
import { useBusiness } from '../stores/queries';

/** B-10 Credits and referrals: the credit account, how payouts work, statements, the standee and referrals. */
export function CreditsPage() {
  const intl = useIntl();
  const current = useCurrentStore();
  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'credits.title' })}</h1>
      {current.status === 'loading' && (
        <div role="status" aria-label={intl.formatMessage({ id: 'credits.loading' })}>
          <Skeleton height="6rem" />
        </div>
      )}
      {current.status === 'error' && (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'credits.error.title' })}
          description={intl.formatMessage({ id: 'credits.error.body' })}
          action={<Button loading={current.retrying} onClick={current.retry}>{intl.formatMessage({ id: 'credits.retry' })}</Button>}
        />
      )}
      {current.status === 'none' && (
        <EmptyState
          icon={StoreIcon}
          title={intl.formatMessage({ id: 'credits.noStore.title' })}
          description={intl.formatMessage({ id: 'credits.noStore.body' })}
          action={
            <Link to="/choose-shop" search={{ next: '/credits' }} className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'credits.noStore.action' })}
            </Link>
          }
        />
      )}
      {current.status === 'ready' &&
        (canSeeCredits(current.store.role) ? (
          <Desk store={current.store} />
        ) : (
          <EmptyState
            icon={Lock}
            title={intl.formatMessage({ id: 'credits.manager.title' })}
            description={intl.formatMessage({ id: 'credits.manager.body' })}
          />
        ))}
    </div>
  );
}

function Desk({ store }: { store: CurrentStore }) {
  const intl = useIntl();
  const account = useCreditAccount(store.bid);
  const referrals = useReferrals(store.bid);
  // D-274: a live shop our team has not checked yet sells and earns, but its payouts wait.
  const biz = useBusiness(store.bid);
  const awaitingCheck = biz.data?.status === 'verified' && biz.data.checkStatus === 'unchecked';
  const lines = useMemo(() => account.data?.pages.flatMap((p) => p.items) ?? [], [account.data]);
  const balance = account.data?.pages[0]?.balance;

  return (
    <>
      <p className={styles.sub}>{store.name}</p>
      <section className="tz-card" aria-labelledby="cr-account-title">
        <div className="tz-card-head">
          <h2 className="tz-card-title" id="cr-account-title">
            {intl.formatMessage({ id: 'credits.account.title' })}
          </h2>
        </div>
        {account.isPending && (
          <div role="status" aria-label={intl.formatMessage({ id: 'credits.loading' })}>
            <Skeleton height="6rem" />
          </div>
        )}
        {account.isError && !account.data && (
          <Banner tone="warning" action={<Button loading={account.isFetching} onClick={() => void account.refetch()}>{intl.formatMessage({ id: 'credits.retry' })}</Button>}>
            {intl.formatMessage({ id: 'credits.account.error' })}
          </Banner>
        )}
        {balance !== undefined && (
          <div className={styles.stack}>
            <div className={styles.balance}>
              <span className={styles.sub}>{intl.formatMessage({ id: 'credits.account.balance' })}</span>
              <span className={styles.balanceValue} data-testid="credit-balance">
                {intl.formatMessage({ id: 'credits.account.credits' }, { count: balance })}
              </span>
              {balance < 0 && <span className={styles.sub}>{intl.formatMessage({ id: 'credits.account.negative' })}</span>}
            </div>
            <CreditLines
              lines={lines}
              hasMore={account.hasNextPage}
              loadingMore={account.isFetchingNextPage}
              onMore={() => void account.fetchNextPage()}
            />
            {account.isError && account.data && (
              <p role="alert" className="tz-field-error">
                {intl.formatMessage({ id: 'credits.lines.error' })}
              </p>
            )}
          </div>
        )}
      </section>
      <PayoutsCard awaitingCheck={awaitingCheck} />
      <StatementsCard bid={store.bid} awaitingCheck={awaitingCheck} />
      <StandeeCard store={store} />
      <ReferralCard
        referrals={referrals.data}
        loading={referrals.isPending}
        failed={referrals.isError}
        retry={() => void referrals.refetch()}
        retrying={referrals.isFetching}
      />
    </>
  );
}
