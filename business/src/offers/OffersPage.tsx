import { Banner, Button, EmptyState, Skeleton, Tabs } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { Inbox, Plus, Store as StoreIcon } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { useCurrentStore, type CurrentStore } from '../orders/store';
import { BannerSheet } from './BannerSheet';
import { BannersPanel } from './BannersPanel';
import { ResolveLink } from './ResolveLink';
import type { OffersSearch } from './deeplink';
import type { OfferDraft } from './form';
import { OfferList } from './OfferList';
import { OfferSheet } from './OfferSheet';
import { canManageOffers } from './permissions';
import styles from './offers.module.css';

type Tab = 'offers' | 'banners';

/** B-08 Offers and banners: owner and manager only (the API is the real gate). */
export function OffersPage({ search = {} }: { search?: OffersSearch }) {
  const intl = useIntl();
  const current = useCurrentStore();
  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'offers.title' })}</h1>
      {current.status === 'loading' && (
        <div role="status" aria-label={intl.formatMessage({ id: 'offers.loading' })}>
          <Skeleton height="6rem" />
        </div>
      )}
      {current.status === 'error' && (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'offers.error.title' })}
          description={intl.formatMessage({ id: 'offers.error.body' })}
          action={<Button onClick={current.retry}>{intl.formatMessage({ id: 'offers.error.retry' })}</Button>}
        />
      )}
      {current.status === 'none' && (
        <EmptyState
          icon={StoreIcon}
          title={intl.formatMessage({ id: 'offers.noStore.title' })}
          description={intl.formatMessage({ id: 'offers.noStore.body' })}
          action={
            <Link to="/choose-shop" search={{ next: '/offers' }} className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'offers.noStore.action' })}
            </Link>
          }
        />
      )}
      {current.status === 'ready' &&
        (canManageOffers(current.store.role) ? (
          <OffersDesk store={current.store} search={search} />
        ) : (
          <Banner tone="info">{intl.formatMessage({ id: 'offers.askManager' })}</Banner>
        ))}
    </div>
  );
}

function OffersDesk({ store, search }: { store: CurrentStore; search: OffersSearch }) {
  const intl = useIntl();
  const [tab, setTab] = useState<Tab>('offers');
  const [offerForm, setOfferForm] = useState<{ initial?: Partial<OfferDraft> } | null>(null);
  const [bannerForm, setBannerForm] = useState(false);

  const newOffer = () => setOfferForm({});
  const panel = tab === 'offers' ? <OfferList store={store} onNew={newOffer} /> : <BannersPanel store={store} onNew={() => setBannerForm(true)} />;

  return (
    <>
      <p className={styles.sub}>{store.name}</p>
      <div className={styles.bar}>
        <Tabs
          label={intl.formatMessage({ id: 'offers.tabs.label' })}
          value={tab}
          onChange={setTab}
          tabs={[
            { value: 'offers', label: intl.formatMessage({ id: 'offers.tabs.offers' }) },
            { value: 'banners', label: intl.formatMessage({ id: 'offers.tabs.banners' }) },
          ]}
        />
        <Button variant="primary" icon={Plus} onClick={() => (tab === 'offers' ? newOffer() : setBannerForm(true))}>
          {intl.formatMessage({ id: tab === 'offers' ? 'offers.new' : 'offers.banner.new' })}
        </Button>
      </div>
      {panel}
      <ResolveLink store={store} search={search} onOpen={(initial: Partial<OfferDraft>) => setOfferForm({ initial })} />
      {offerForm && <OfferSheet store={store} initial={offerForm.initial} onClose={() => setOfferForm(null)} />}
      {bannerForm && <BannerSheet store={store} onClose={() => setBannerForm(false)} />}
    </>
  );
}
