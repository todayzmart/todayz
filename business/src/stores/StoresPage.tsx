import type { Store } from '@todayz/api-client/gen/model/store';
import { createStore, getStore, submitStore, updateStore } from '@todayz/api-client/gen/business/business';
import { Banner, Button, EmptyState, Sheet, Skeleton, StatusPill, useToast, type PillTone } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Inbox, Lock, Plus, Store as StoreIcon } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import type { BusinessSummary, ShopRole } from '../app/shop';
import { BizError } from './BizError';
import { canManageStores, canOnboard } from './perm';
import { StaffSection } from './StaffSection';
import { StoreForm } from './StoreForm';
import { createPayload, updatePayload, type StoreValues } from './storeValues';
import {
  bizKey,
  businessesKey,
  storePath,
  storesKey,
  useAction,
  useCurrentBusiness,
  useIfMatch,
  useStores,
} from './queries';
import styles from './stores.module.css';

const STORE_TONE: Record<Store['status'], PillTone> = {
  draft: 'neutral',
  pending_review: 'pending',
  live: 'ready',
  paused: 'cancelled',
  suspended: 'rejected',
};

/** B-13 Stores and staff: the shop's stores and the people who work in them. Owners and managers only. */
export function StoresPage() {
  const intl = useIntl();
  const cur = useCurrentBusiness();
  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'stores.title' })}</h1>
      {cur.status === 'loading' && (
        <div role="status" aria-label={intl.formatMessage({ id: 'stores.loading' })}>
          <Skeleton height="6rem" />
        </div>
      )}
      {cur.status === 'error' && (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'stores.loadFailed.title' })}
          description={intl.formatMessage({ id: 'stores.loadFailed.body' })}
          action={<Button loading={cur.retrying} onClick={cur.retry}>{intl.formatMessage({ id: 'stores.retry' })}</Button>}
        />
      )}
      {cur.status === 'none' && (
        <EmptyState
          icon={StoreIcon}
          title={intl.formatMessage({ id: 'stores.none.title' })}
          description={intl.formatMessage({ id: 'stores.none.body' })}
          action={
            <Link to="/onboarding" className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'stores.none.action' })}
            </Link>
          }
        />
      )}
      {cur.status === 'ready' &&
        (canManageStores(cur.business.role) ? (
          <StoresDesk business={cur.business} />
        ) : (
          <EmptyState
            icon={Lock}
            title={intl.formatMessage({ id: 'stores.noAccess.title' })}
            description={intl.formatMessage({ id: 'stores.noAccess.body' })}
          />
        ))}
    </div>
  );
}

function StoresDesk({ business }: { business: BusinessSummary }) {
  const intl = useIntl();
  const bid = business.id;
  const role: ShopRole = business.role;
  const stores = useStores(bid);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<Store | null>(null);
  const items = stores.data?.items ?? [];
  return (
    <>
      <Banner tone={business.status === 'verified' ? 'info' : 'warning'} title={intl.formatMessage({ id: `stores.business.${business.status}` })}>
        {business.status === 'draft' || business.status === 'needs_info' ? (
          canOnboard(role) && (
            <Link to="/onboarding" className="tz-btn">
              {intl.formatMessage({ id: 'stores.business.continue' })}
            </Link>
          )
        ) : null}
      </Banner>

      <section className="tz-card" aria-labelledby="stores-h">
        <div className={styles.head}>
          <h2 id="stores-h" className="tz-card-title">
            {intl.formatMessage({ id: 'stores.section.stores' })}
          </h2>
          <Button icon={Plus} onClick={() => setAdding(true)}>
            {intl.formatMessage({ id: 'stores.add' })}
          </Button>
        </div>
        {stores.isLoading && <Skeleton height="4rem" />}
        {stores.isError && <BizError error={stores.error} />}
        {stores.data && items.length === 0 && <p className="tz-card-desc">{intl.formatMessage({ id: 'stores.empty' })}</p>}
        <ul className={styles.list}>
          {items.map((s) => (
            <StoreRow key={s.id} store={s} bid={bid} onEdit={() => setEditing(s)} />
          ))}
        </ul>
        <p className={styles.footer}>{intl.formatMessage({ id: 'stores.support' })}</p>
      </section>

      <StaffSection business={business} stores={items} />

      {adding && <AddStoreSheet bid={bid} onClose={() => setAdding(false)} />}
      {editing && <EditStoreSheet bid={bid} store={editing} onClose={() => setEditing(null)} />}
    </>
  );
}

function address(s: Store): string {
  return [s.locality, s.city, s.pincode].filter(Boolean).join(', ');
}

function StoreRow({ store, bid, onEdit }: { store: Store; bid: string; onEdit: () => void }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const action = useAction();
  const send = async () => {
    const out = await action.run(`submit-${store.id}`, (key) => submitStore(bid, store.id, { idempotencyKey: key }));
    if (!out) return;
    await qc.invalidateQueries({ queryKey: storesKey(bid) });
    await qc.invalidateQueries({ queryKey: businessesKey });
    toast.show({ message: intl.formatMessage({ id: 'stores.submitted' }) });
  };
  return (
    <li className={styles.stack}>
      <div className={styles.row}>
        <div className={styles.rowMain}>
          <span className={styles.rowName}>{store.name}</span>
          {address(store) && <span className={styles.meta}>{address(store)}</span>}
        </div>
        <div className={styles.pills}>
          <StatusPill tone={STORE_TONE[store.status]}>{intl.formatMessage({ id: `stores.status.${store.status}` })}</StatusPill>
          <Button aria-label={intl.formatMessage({ id: 'stores.editNamed' }, { name: store.name })} onClick={onEdit}>
            {intl.formatMessage({ id: 'stores.edit' })}
          </Button>
          {store.status === 'draft' && (
            <Button
              variant="primary"
              loading={action.pending}
              aria-label={intl.formatMessage({ id: 'stores.submitNamed' }, { name: store.name })}
              onClick={() => void send()}
            >
              {intl.formatMessage({ id: 'stores.submit' })}
            </Button>
          )}
        </div>
      </div>
      <BizError error={action.error} />
    </li>
  );
}

function AddStoreSheet({ bid, onClose }: { bid: string; onClose: () => void }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const action = useAction();
  const save = async (v: StoreValues) => {
    const out = await action.run('create-store', (key) => createStore(bid, createPayload(v), { idempotencyKey: key }));
    if (!out) return;
    await qc.invalidateQueries({ queryKey: storesKey(bid) });
    await qc.invalidateQueries({ queryKey: businessesKey });
    toast.show({ message: intl.formatMessage({ id: 'stores.saved' }) });
    onClose();
  };
  return (
    <Sheet
      open
      onClose={onClose}
      title={intl.formatMessage({ id: 'stores.addTitle' })}
      description={intl.formatMessage({ id: 'stores.addBody' })}
      closeLabel={intl.formatMessage({ id: 'stores.sheet.close' })}
    >
      <StoreForm
        submitLabel={intl.formatMessage({ id: 'stores.create' })}
        pending={action.pending}
        error={action.error}
        secondary={<Button onClick={onClose}>{intl.formatMessage({ id: 'stores.cancel' })}</Button>}
        onSubmit={(v) => void save(v)}
      />
    </Sheet>
  );
}

function EditStoreSheet({ bid, store, onClose }: { bid: string; store: Store; onClose: () => void }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const action = useAction();
  const ifMatch = useIfMatch();
  const save = async (v: StoreValues, before: StoreValues) => {
    const body = updatePayload(v, before);
    if (Object.keys(body).length === 0) {
      onClose();
      return;
    }
    const out = await action.run(`edit-${store.id}`, async () => {
      const tag = await ifMatch(storePath(bid, store.id), () => getStore(bid, store.id));
      return updateStore(bid, store.id, body, tag ? { ifMatch: tag } : {});
    });
    if (!out) return;
    await qc.invalidateQueries({ queryKey: storesKey(bid) });
    await qc.invalidateQueries({ queryKey: bizKey(bid) });
    toast.show({ message: intl.formatMessage({ id: 'stores.saved' }) });
    onClose();
  };
  return (
    <Sheet
      open
      onClose={onClose}
      title={intl.formatMessage({ id: 'stores.editTitle' })}
      description={intl.formatMessage({ id: 'stores.editBody' })}
      closeLabel={intl.formatMessage({ id: 'stores.sheet.close' })}
    >
      <StoreForm
        store={store}
        submitLabel={intl.formatMessage({ id: 'stores.save' })}
        pending={action.pending}
        error={action.error}
        secondary={<Button onClick={onClose}>{intl.formatMessage({ id: 'stores.cancel' })}</Button>}
        onSubmit={(v, before) => void save(v, before)}
      />
    </Sheet>
  );
}
