import { createBusiness, createStore, getBusiness, submitBusiness, updateBusiness } from '@todayz/api-client/gen/business/business';
import type { Business } from '@todayz/api-client/gen/model/business';
import { ApiError } from '@todayz/api-client';
import { Banner, Button, EmptyState, Skeleton, StatusPill, TextField, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Check, Inbox, Lock } from 'lucide-react';
import { useState, type ReactNode } from 'react';
import { useIntl } from 'react-intl';
import { rememberStore, type BusinessSummary } from '../app/shop';
import { HoursSection, PayoutSection } from '../settings/sections';
import { BizError } from '../stores/BizError';
import { canOnboard } from '../stores/perm';
import {
  bizKey,
  businessPath,
  businessesKey,
  fieldName,
  storesKey,
  useAction,
  useBusiness,
  useBusinesses,
  useCategories,
  useCurrentBusiness,
  useIfMatch,
  useKyc,
  useStoreSettings,
  useStores,
} from '../stores/queries';
import { StoreForm } from '../stores/StoreForm';
import { createPayload, type StoreValues } from '../stores/storeValues';
import styles from '../stores/stores.module.css';
import { DocumentsStep } from './DocumentsStep';

/** B-02 Shop setup: business details, first store, hours, payout, documents, then send for verification. Owner only. */
export function OnboardingPage() {
  const intl = useIntl();
  const list = useBusinesses();
  const cur = useCurrentBusiness();
  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'onboarding.title' })}</h1>
      {cur.status === 'loading' && (
        <div role="status" aria-label={intl.formatMessage({ id: 'onboarding.loading' })}>
          <Skeleton height="6rem" />
        </div>
      )}
      {cur.status === 'error' && (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'onboarding.loadFailed.title' })}
          description={intl.formatMessage({ id: 'onboarding.loadFailed.body' })}
          action={<Button onClick={cur.retry}>{intl.formatMessage({ id: 'onboarding.retry' })}</Button>}
        />
      )}
      {cur.status === 'none' && <Wizard business={null} refetchList={() => void list.refetch()} />}
      {cur.status === 'ready' &&
        (canOnboard(cur.business.role) ? (
          <Wizard business={cur.business} refetchList={() => void list.refetch()} />
        ) : (
          <EmptyState
            icon={Lock}
            title={intl.formatMessage({ id: 'onboarding.ownerOnly.title' })}
            description={intl.formatMessage({ id: 'onboarding.ownerOnly.body' })}
          />
        ))}
    </div>
  );
}

function Step({ n, title, done, children }: { n: number; title: string; done: boolean; children: ReactNode }) {
  const intl = useIntl();
  return (
    <section className="tz-card" aria-label={title}>
      <div className={styles.head}>
        <h2 className="tz-card-title">{intl.formatMessage({ id: 'onboarding.step' }, { n, title })}</h2>
        {done && (
          <StatusPill tone="accepted">
            <Check size={14} aria-hidden="true" /> {intl.formatMessage({ id: 'onboarding.done' })}
          </StatusPill>
        )}
      </div>
      {children}
    </section>
  );
}

function Wizard({ business, refetchList }: { business: BusinessSummary | null; refetchList: () => void }) {
  const intl = useIntl();
  const bid = business?.id ?? null;
  return (
    <>
      <Step n={1} title={intl.formatMessage({ id: 'onboarding.business.title' })} done={bid !== null}>
        <BusinessStep bid={bid} onCreated={refetchList} />
      </Step>
      {bid && <AfterBusiness bid={bid} />}
    </>
  );
}

function AfterBusiness({ bid }: { bid: string }) {
  const intl = useIntl();
  const biz = useBusiness(bid);
  const stores = useStores(bid);
  const first = stores.data?.items[0];
  const settings = useStoreSettings(bid, first?.id ?? '', Boolean(first));
  const kyc = useKyc(bid);
  const status = biz.data?.status;
  const editable = status === 'draft' || status === 'needs_info';
  const hoursSet = Boolean(settings.data && Object.values(settings.data.hours.weekly).some((d) => d && d.length > 0));
  return (
    <>
      {biz.data && <StatusBanner business={biz.data} />}
      <Step n={2} title={intl.formatMessage({ id: 'onboarding.store.title' })} done={Boolean(first)}>
        {stores.isLoading && <Skeleton height="4rem" />}
        {stores.isError && <BizError error={stores.error} />}
        {stores.data && !first && editable && <FirstStore bid={bid} />}
        {first && (
          <p className="tz-card-desc">
            {intl.formatMessage({ id: 'onboarding.store.have' }, { name: first.name })}{' '}
            <Link to="/stores">{intl.formatMessage({ id: 'onboarding.store.manage' })}</Link>
          </p>
        )}
      </Step>
      {first && settings.data && editable && (
        <Step n={3} title={intl.formatMessage({ id: 'onboarding.hours.title' })} done={hoursSet}>
          <HoursSection bid={bid} sid={first.id} settings={settings.data} />
        </Step>
      )}
      <Step n={4} title={intl.formatMessage({ id: 'onboarding.payout.title' })} done={Boolean(kyc.data && kyc.data.status !== 'missing')}>
        <p className="tz-card-desc">{intl.formatMessage({ id: 'onboarding.payout.body' })}</p>
        <PayoutSection bid={bid} canEdit />
      </Step>
      <Step n={5} title={intl.formatMessage({ id: 'onboarding.docs.title' })} done={false}>
        <DocumentsStep bid={bid} editable={editable} />
      </Step>
      {editable && (
        <Step n={6} title={intl.formatMessage({ id: 'onboarding.submit.title' })} done={false}>
          <SubmitStep bid={bid} />
        </Step>
      )}
    </>
  );
}

function StatusBanner({ business }: { business: Business }) {
  const intl = useIntl();
  const { status, infoRequest } = business;
  if (status === 'draft') return null;
  const fields = infoRequest?.fields ?? [];
  return (
    <Banner tone={status === 'verified' || status === 'submitted' ? 'info' : 'warning'} title={intl.formatMessage({ id: `onboarding.status.${status}.title` })}>
      {intl.formatMessage({ id: `onboarding.status.${status}.body` })}
      {status === 'needs_info' && fields.length > 0 && (
        <ul>
          {fields.map((f) => (
            <li key={f}>{fieldLabel(intl, f)}</li>
          ))}
        </ul>
      )}
      {status === 'needs_info' && infoRequest?.note && <p>{infoRequest.note}</p>}
    </Banner>
  );
}

/** A field name from the server in the shop's words; one we have no word for is shown as the generic line. */
function fieldLabel(intl: ReturnType<typeof useIntl>, path: string): string {
  const id = `onboarding.field.${fieldName(path)}`;
  return id in intl.messages ? intl.formatMessage({ id }) : intl.formatMessage({ id: 'onboarding.field.other' });
}

function BusinessStep({ bid, onCreated }: { bid: string | null; onCreated: () => void }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const action = useAction();
  const ifMatch = useIfMatch();
  const biz = useBusiness(bid ?? '', bid !== null);
  const cats = useCategories();
  const loaded = bid === null || biz.data;
  if (!loaded) return biz.isError ? <BizError error={biz.error} /> : <Skeleton height="6rem" />;
  const editable = bid === null || biz.data?.status === 'draft' || biz.data?.status === 'needs_info';
  return (
    <BusinessForm
      key={biz.data?.id ?? 'new'}
      initial={{ brandName: biz.data?.brandName ?? '', legalName: biz.data?.legalName ?? '', categoryId: biz.data?.categoryId ?? '' }}
      categories={cats.data?.items.map((c) => ({ id: c.id, name: c.name })) ?? []}
      editable={editable}
      pending={action.pending}
      error={action.error}
      onSave={async (v) => {
        const out = bid
          ? await action.run('biz-update', async () => {
              const tag = await ifMatch(businessPath(bid), () => getBusiness(bid));
              return updateBusiness(
                bid,
                { brandName: v.brandName, legalName: v.legalName || null, categoryId: v.categoryId || null },
                tag ? { ifMatch: tag } : {},
              );
            })
          : await action.run('biz-create', (key) =>
              createBusiness(
                { brandName: v.brandName, ...(v.legalName ? { legalName: v.legalName } : {}), ...(v.categoryId ? { categoryId: v.categoryId } : {}) },
                { idempotencyKey: key },
              ),
            );
        if (!out) return;
        await qc.invalidateQueries({ queryKey: businessesKey });
        if (bid) await qc.invalidateQueries({ queryKey: bizKey(bid) });
        else onCreated();
        toast.show({ message: intl.formatMessage({ id: 'onboarding.saved' }) });
      }}
    />
  );
}

interface BusinessValues {
  brandName: string;
  legalName: string;
  categoryId: string;
}

function BusinessForm(props: {
  initial: BusinessValues;
  categories: { id: string; name: string }[];
  editable: boolean;
  pending: boolean;
  error: unknown;
  onSave: (v: BusinessValues) => Promise<void>;
}) {
  const intl = useIntl();
  const [v, setV] = useState(props.initial);
  const [shown, setShown] = useState(false);
  const nameMissing = v.brandName.trim() === '';
  return (
    <form
      className={styles.stack}
      noValidate
      onSubmit={(e) => {
        e.preventDefault();
        setShown(true);
        if (!nameMissing) void props.onSave({ ...v, brandName: v.brandName.trim(), legalName: v.legalName.trim() });
      }}
    >
      <TextField
        label={intl.formatMessage({ id: 'onboarding.business.brandName' })}
        help={intl.formatMessage({ id: 'onboarding.business.brandNameHelp' })}
        value={v.brandName}
        maxLength={80}
        disabled={!props.editable}
        error={shown && nameMissing ? intl.formatMessage({ id: 'onboarding.business.brandNameRequired' }) : undefined}
        onChange={(e) => setV((p) => ({ ...p, brandName: e.target.value }))}
        autoComplete="off"
      />
      <TextField
        label={intl.formatMessage({ id: 'onboarding.business.legalName' })}
        help={intl.formatMessage({ id: 'onboarding.business.legalNameHelp' })}
        value={v.legalName}
        maxLength={120}
        disabled={!props.editable}
        onChange={(e) => setV((p) => ({ ...p, legalName: e.target.value }))}
        autoComplete="off"
      />
      <div className="tz-field">
        <label className="tz-field-label" htmlFor="onb-category">
          {intl.formatMessage({ id: 'onboarding.business.category' })}
        </label>
        <select
          id="onb-category"
          className={`tz-input ${styles.select}`}
          value={v.categoryId}
          disabled={!props.editable}
          onChange={(e) => setV((p) => ({ ...p, categoryId: e.target.value }))}
        >
          <option value="">{intl.formatMessage({ id: 'onboarding.business.categoryNone' })}</option>
          {props.categories.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name}
            </option>
          ))}
        </select>
      </div>
      <BizError error={props.error} />
      {props.editable && (
        <div className={styles.actions}>
          <Button type="submit" variant="primary" loading={props.pending}>
            {intl.formatMessage({ id: 'onboarding.save' })}
          </Button>
        </div>
      )}
    </form>
  );
}

function FirstStore({ bid }: { bid: string }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const action = useAction();
  const save = async (v: StoreValues) => {
    const out = await action.run('create-store', (key) => createStore(bid, createPayload(v), { idempotencyKey: key }));
    if (!out) return;
    rememberStore(out.id);
    await qc.invalidateQueries({ queryKey: storesKey(bid) });
    await qc.invalidateQueries({ queryKey: businessesKey });
    toast.show({ message: intl.formatMessage({ id: 'onboarding.saved' }) });
  };
  return (
    <StoreForm
      submitLabel={intl.formatMessage({ id: 'onboarding.store.create' })}
      pending={action.pending}
      error={action.error}
      onSubmit={(v) => void save(v)}
    />
  );
}

function SubmitStep({ bid }: { bid: string }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const action = useAction();
  const send = async () => {
    const out = await action.run('submit-business', (key) => submitBusiness(bid, { idempotencyKey: key }));
    if (!out) return;
    await qc.invalidateQueries({ queryKey: bizKey(bid) });
    await qc.invalidateQueries({ queryKey: businessesKey });
    toast.show({ message: intl.formatMessage({ id: 'onboarding.submit.sent' }) });
  };
  const missing = action.error instanceof ApiError && action.error.code === 'submit_incomplete' ? Object.keys(action.error.fields ?? {}) : [];
  return (
    <div className={styles.stack}>
      <p className="tz-card-desc">{intl.formatMessage({ id: 'onboarding.submit.body' })}</p>
      {missing.length > 0 ? (
        <div role="alert" className="tz-field-error">
          <p>{intl.formatMessage({ id: 'stores.error.submit_incomplete' })}</p>
          <ul>
            {[...new Set(missing.map((m) => fieldLabel(intl, m)))].map((label) => (
              <li key={label}>{label}</li>
            ))}
          </ul>
        </div>
      ) : (
        <BizError error={action.error} />
      )}
      <div className={styles.actions}>
        <Button variant="primary" loading={action.pending} onClick={() => void send()}>
          {intl.formatMessage({ id: 'onboarding.submit.action' })}
        </Button>
      </div>
    </div>
  );
}
