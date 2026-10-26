import { useSessionState } from '@todayz/auth';
import { createFileRoute, useRouter } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { Button, Skeleton } from '@todayz/ui';
import { useEffect } from 'react';
import { useIntl } from 'react-intl';
import { safeNext, validateNextSearch } from '../app/next';
import { listBusinesses, rememberStore, rememberedStore, type ShopRole } from '../app/shop';
import styles from '../shell/shell.module.css';

export const Route = createFileRoute('/choose-shop')({
  validateSearch: validateNextSearch,
  component: ChooseShop,
});

const ROLE_LABEL: Record<ShopRole, string> = {
  owner: 'shop.pick.role.owner',
  manager: 'shop.pick.role.manager',
  cashier: 'shop.pick.role.cashier',
};

/**
 * B-01 picker: several businesses or stores, role shown, last store remembered. One store (or the
 * remembered one) goes straight on. A failed load shows a way on to the overview instead of
 * blocking the app.
 */
function ChooseShop() {
  const intl = useIntl();
  const router = useRouter();
  const { next } = Route.useSearch();
  const { status } = useSessionState();
  const query = useQuery({ queryKey: ['businesses'], queryFn: listBusinesses, enabled: status === 'signedIn', retry: false });

  const go = () => router.history.replace(safeNext(next));
  const businesses = query.data?.items ?? [];
  const stores = businesses.flatMap((b) => b.stores);
  const remembered = rememberedStore();
  const auto = stores.length === 1 ? stores[0] : stores.find((s) => s.id === remembered);

  useEffect(() => {
    if (query.data && auto) {
      rememberStore(auto.id);
      router.history.replace(safeNext(next));
    }
  }, [query.data, auto, router, next]);

  return (
    <div className={styles.stack}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'shop.pick.title' })}</h1>
      <p className={styles.sub}>{intl.formatMessage({ id: 'shop.pick.sub' })}</p>
      {(query.isPending || (query.data && auto)) && (
        <div role="status" aria-label={intl.formatMessage({ id: 'shop.pick.loading' })}>
          <Skeleton height="6rem" />
        </div>
      )}
      {query.isError && (
        <div role="alert" className={styles.stack}>
          <p>{intl.formatMessage({ id: 'shop.pick.unavailable' })}</p>
          <Button variant="primary" onClick={go}>
            {intl.formatMessage({ id: 'shop.pick.continue' })}
          </Button>
          <Button variant="ghost" loading={query.isFetching} onClick={() => void query.refetch()}>
            {intl.formatMessage({ id: 'shop.pick.retry' })}
          </Button>
        </div>
      )}
      {query.data && businesses.length === 0 && <p role="status">{intl.formatMessage({ id: 'shop.pick.empty' })}</p>}
      {query.data &&
        !auto &&
        businesses.map((b) => (
          <section key={b.id} className={`tz-card ${styles.stack}`} aria-label={intl.formatMessage({ id: 'shop.pick.group' }, { business: b.brandName })}>
            <h2 className="tz-card-title">
              {b.brandName} <span className="tz-pill">{intl.formatMessage({ id: ROLE_LABEL[b.role] })}</span>
            </h2>
            {b.stores.map((s) => (
              <Button
                key={s.id}
                block
                onClick={() => {
                  rememberStore(s.id);
                  go();
                }}
              >
                {s.name}
              </Button>
            ))}
          </section>
        ))}
    </div>
  );
}
