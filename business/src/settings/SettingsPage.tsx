import { Banner, Button, EmptyState, Skeleton } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { Inbox, Lock, Store as StoreIcon } from 'lucide-react';
import { useIntl } from 'react-intl';
import { rememberedStore, type BusinessSummary } from '../app/shop';
import { BizError } from '../stores/BizError';
import { canEditPayout, canManageStores, canMuteAsks, canOnboard, canSeePayout } from '../stores/perm';
import { useCurrentBusiness, useStore, useStoreSettings } from '../stores/queries';
import styles from '../stores/stores.module.css';
import { DeliverySection, HoursSection, PayoutSection, ProfileSection, RewardsSection } from './sections';

/** B-14 Settings: profile, hours, pickup and delivery, rewards and payout, for the store this device works in. */
export function SettingsPage() {
  const intl = useIntl();
  const cur = useCurrentBusiness();
  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'settings.title' })}</h1>
      {cur.status === 'loading' && (
        <div role="status" aria-label={intl.formatMessage({ id: 'settings.loading' })}>
          <Skeleton height="6rem" />
        </div>
      )}
      {cur.status === 'error' && (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'settings.loadFailed.title' })}
          description={intl.formatMessage({ id: 'settings.loadFailed.body' })}
          action={<Button onClick={cur.retry}>{intl.formatMessage({ id: 'settings.retry' })}</Button>}
        />
      )}
      {cur.status === 'none' && (
        <EmptyState
          icon={StoreIcon}
          title={intl.formatMessage({ id: 'settings.none.title' })}
          description={intl.formatMessage({ id: 'settings.none.body' })}
          action={
            <Link to="/onboarding" className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'settings.none.action' })}
            </Link>
          }
        />
      )}
      {cur.status === 'ready' &&
        (canManageStores(cur.business.role) ? (
          <SettingsDesk business={cur.business} />
        ) : (
          <EmptyState
            icon={Lock}
            title={intl.formatMessage({ id: 'settings.noAccess.title' })}
            description={intl.formatMessage({ id: 'settings.noAccess.body' })}
          />
        ))}
    </div>
  );
}

function SettingsDesk({ business }: { business: BusinessSummary }) {
  const intl = useIntl();
  const bid = business.id;
  const role = business.role;
  const remembered = rememberedStore();
  const sid = (business.stores.find((s) => s.id === remembered) ?? business.stores[0])?.id;
  return (
    <>
      <nav className={styles.actions} aria-label={intl.formatMessage({ id: 'settings.links' })}>
        <Link to="/stores" className="tz-btn">
          {intl.formatMessage({ id: 'settings.link.stores' })}
        </Link>
        {canOnboard(role) && (business.status === 'draft' || business.status === 'needs_info') && (
          <Link to="/onboarding" className="tz-btn">
            {intl.formatMessage({ id: 'settings.link.onboarding' })}
          </Link>
        )}
      </nav>
      {sid ? (
        <StoreSettingsDesk bid={bid} sid={sid} role={role} />
      ) : (
        <Banner tone="warning" title={intl.formatMessage({ id: 'settings.noStore.title' })}>
          {intl.formatMessage({ id: 'settings.noStore.body' })}
        </Banner>
      )}
      {canSeePayout(role) && <PayoutSection bid={bid} canEdit={canEditPayout(role)} />}
    </>
  );
}

function StoreSettingsDesk({ bid, sid, role }: { bid: string; sid: string; role: BusinessSummary['role'] }) {
  const intl = useIntl();
  const store = useStore(bid, sid);
  const settings = useStoreSettings(bid, sid);
  const error = store.error ?? settings.error;
  if (error) return <BizError error={error} />;
  if (!store.data || !settings.data) {
    return (
      <div role="status" aria-label={intl.formatMessage({ id: 'settings.loading' })}>
        <Skeleton height="10rem" />
      </div>
    );
  }
  return (
    <>
      <p className="tz-card-desc">{intl.formatMessage({ id: 'settings.forStore' }, { name: store.data.name })}</p>
      <ProfileSection bid={bid} store={store.data} />
      <HoursSection bid={bid} sid={sid} settings={settings.data} />
      <DeliverySection bid={bid} sid={sid} settings={settings.data} />
      <RewardsSection bid={bid} sid={sid} settings={settings.data} canMuteAsks={canMuteAsks(role)} />
    </>
  );
}
