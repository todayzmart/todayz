import { createBusiness, createStore, getBusiness, submitBusiness, updateBusiness } from '@todayz/api-client/gen/business/business';
import type { Business } from '@todayz/api-client/gen/model/business';
import { ApiError } from '@todayz/api-client';
import { Banner, Button, EmptyState, Skeleton, TextField, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { ArrowLeft, ArrowRight, Check, Inbox, LayoutDashboard, Lock } from 'lucide-react';
import { useState, type ReactNode } from 'react';
import { useIntl, type IntlShape } from 'react-intl';
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
  useDocuments,
  useIfMatch,
  useKyc,
  useStoreSettings,
  useStores,
} from '../stores/queries';
import { StoreForm } from '../stores/StoreForm';
import { createPayload, type StoreValues } from '../stores/storeValues';
import stores from '../stores/stores.module.css';
import { DocumentsStep } from './DocumentsStep';
import { PHOTO_KINDS, TAX_KINDS } from './documents';
import styles from './onboarding.module.css';
import { firstOpen, OPTIONAL, stepFrom, stepStates, STEPS, type StepId, type StepState } from './steps';

/**
 * B-02 Shop setup as a step flow with a dark green rail (D-274, a step-rail layout from a reference the
 * founder shared; our own copy). The shop fills in its details and goes live at once: nothing is
 * approved first. Our team checks the shop later; payouts wait for that check. Owner only.
 * Drawn without the shell (Gate's BARE list); the rail is the page's own navigation.
 */
export function OnboardingPage() {
  const intl = useIntl();
  const list = useBusinesses();
  const cur = useCurrentBusiness();
  if (cur.status === 'loading') {
    return (
      <Frame steps={null} active={null} onPick={noop}>
        <div role="status" aria-label={intl.formatMessage({ id: 'onboarding.loading' })}>
          <Skeleton height="12rem" />
        </div>
      </Frame>
    );
  }
  if (cur.status === 'error') {
    return (
      <Frame steps={null} active={null} onPick={noop}>
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'onboarding.loadFailed.title' })}
          description={intl.formatMessage({ id: 'onboarding.loadFailed.body' })}
          action={<Button loading={cur.retrying} onClick={cur.retry}>{intl.formatMessage({ id: 'onboarding.retry' })}</Button>}
        />
      </Frame>
    );
  }
  if (cur.status === 'ready' && !canOnboard(cur.business.role)) {
    return (
      <Frame steps={null} active={null} onPick={noop} home>
        <EmptyState
          icon={Lock}
          title={intl.formatMessage({ id: 'onboarding.ownerOnly.title' })}
          description={intl.formatMessage({ id: 'onboarding.ownerOnly.body' })}
        />
      </Frame>
    );
  }
  const business = cur.status === 'ready' ? cur.business : null;
  return business ? (
    <Flow key={business.id} business={business} />
  ) : (
    <NewShop refetchList={() => void list.refetch()} />
  );
}

const noop = () => undefined;

function stepName(intl: IntlShape, id: StepId): string {
  return intl.formatMessage({ id: `onboarding.steps.${id}` });
}

/**
 * The page frame: the green rail (desktop, a full-height column; phone, a header with the steps as
 * a scrolling row) and the step content. `steps` is null while there is nothing to list. `home` shows
 * "Go to dashboard", only once there is a live shop to go to (a new signup has none).
 */
function Frame(props: { steps: StepState[] | null; active: StepId | null; onPick: (id: StepId) => void; home?: boolean; children: ReactNode }) {
  const { steps, active, onPick, home = false, children } = props;
  const intl = useIntl();
  const at = active ? STEPS.indexOf(active) : -1;
  const done = steps?.filter((s) => s.done && !OPTIONAL.has(s.id)).length ?? 0;
  const needed = STEPS.length - OPTIONAL.size;
  return (
    <div className={styles.onb} data-skin="bright">
      <a className={styles.skip} href="#main">
        {intl.formatMessage({ id: 'shell.skipToContent' })}
      </a>
      <aside className={styles.rail}>
        <div className={styles.brand}>
          <img className={styles.mark} src="/icons/mark-96.png" alt="" width={24} height={24} />
          <span>
            Today<b>Z</b>
          </span>
          <span className={styles.tag}>{intl.formatMessage({ id: 'shell.appTag' })}</span>
        </div>
        <div className={styles.intro}>
          <h1 className={styles.title}>{intl.formatMessage({ id: 'onboarding.title' })}</h1>
          <p className={styles.lede}>{intl.formatMessage({ id: 'onboarding.lede' })}</p>
        </div>
        {steps && (
          <>
            <p className={styles.progressText}>{intl.formatMessage({ id: 'onboarding.progress' }, { done, total: needed })}</p>
            <div className={styles.bar} aria-hidden="true">
              <i style={{ width: `${Math.round((done / needed) * 100)}%` }} />
            </div>
            <nav aria-label={intl.formatMessage({ id: 'onboarding.steps.label' })}>
              <ol className={styles.steps}>
                {steps.map((s, i) => (
                  <li key={s.id}>
                    <button
                      type="button"
                      className={styles.step}
                      aria-current={s.id === active ? 'step' : undefined}
                      disabled={!s.reachable}
                      onClick={() => onPick(s.id)}
                    >
                      <span className={styles.disc} data-done={s.done || undefined} aria-hidden="true">
                        {s.done ? <Check size={14} strokeWidth={3} /> : i + 1}
                      </span>
                      <span className={styles.stepText}>
                        <span className={styles.stepName}>{stepName(intl, s.id)}</span>
                        <span className={styles.stepHint}>
                          {s.done
                            ? intl.formatMessage({ id: 'onboarding.done' })
                            : OPTIONAL.has(s.id)
                              ? intl.formatMessage({ id: 'onboarding.optional' })
                              : s.id === 'photos'
                                ? intl.formatMessage({ id: 'onboarding.recommended' })
                                : ''}
                        </span>
                      </span>
                    </button>
                  </li>
                ))}
              </ol>
            </nav>
          </>
        )}
        {home && (
          <Link to="/" className={styles.home}>
            <LayoutDashboard size={16} aria-hidden="true" />
            {intl.formatMessage({ id: 'onboarding.toDashboard' })}
          </Link>
        )}
      </aside>
      <main id="main" className={styles.content} tabIndex={-1} data-step={at >= 0 ? at + 1 : undefined}>
        {children}
      </main>
    </div>
  );
}

/** One step's card: "Step n of 7", the title, a line on why, the form, then Back and Next. */
function StepCard(props: {
  id: StepId;
  intro: string;
  children: ReactNode;
  onBack?: (() => void) | undefined;
  onNext?: (() => void) | undefined;
  nextLabel?: string | undefined;
}) {
  const intl = useIntl();
  const n = STEPS.indexOf(props.id) + 1;
  const title = stepName(intl, props.id);
  return (
    <section className={`tz-card ${styles.card}`} aria-labelledby={`onb-${props.id}`}>
      <p className={styles.kicker}>
        {intl.formatMessage({ id: 'onboarding.stepOf' }, { n, total: STEPS.length })}
        {OPTIONAL.has(props.id) && ` · ${intl.formatMessage({ id: 'onboarding.optional' })}`}
      </p>
      <h2 id={`onb-${props.id}`} className={styles.cardTitle}>
        {title}
      </h2>
      <p className="tz-card-desc">{props.intro}</p>
      <div className={styles.body}>{props.children}</div>
      {(props.onBack || props.onNext) && (
        <div className={styles.nav}>
          {props.onBack ? (
            <Button variant="ghost" onClick={props.onBack}>
              <ArrowLeft size={16} aria-hidden="true" /> {intl.formatMessage({ id: 'onboarding.back' })}
            </Button>
          ) : (
            <span />
          )}
          {props.onNext && (
            <Button onClick={props.onNext}>
              {props.nextLabel ?? intl.formatMessage({ id: 'onboarding.next' })} <ArrowRight size={16} aria-hidden="true" />
            </Button>
          )}
        </div>
      )}
    </section>
  );
}

/** No business yet: only step 1 is open. */
function NewShop({ refetchList }: { refetchList: () => void }) {
  const intl = useIntl();
  const steps = STEPS.map((id) => ({ id, done: false, reachable: id === 'business' }));
  return (
    <Frame steps={steps} active="business" onPick={noop}>
      <StepCard id="business" intro={intl.formatMessage({ id: 'onboarding.intro.business' })}>
        <BusinessStep bid={null} onCreated={refetchList} />
      </StepCard>
    </Frame>
  );
}

function Flow({ business }: { business: BusinessSummary }) {
  const intl = useIntl();
  const bid = business.id;
  const biz = useBusiness(bid);
  const storeList = useStores(bid);
  const first = storeList.data?.items[0];
  const settings = useStoreSettings(bid, first?.id ?? '', Boolean(first));
  const kyc = useKyc(bid);
  const docs = useDocuments(bid);
  const [picked, setPicked] = useState<StepId | null>(null);

  const status = biz.data?.status;
  const hoursSet = Boolean(settings.data && Object.values(settings.data.hours.weekly).some((d) => d && d.length > 0));
  const photos = docs.data?.items.some((d) => PHOTO_KINDS.includes(d.kind) && (d.status === 'uploaded' || d.status === 'accepted')) ?? false;
  const done: Record<StepId, boolean> = {
    business: Boolean(biz.data),
    store: Boolean(first),
    hours: hoursSet,
    category: Boolean(biz.data?.categoryId),
    photos,
    payout: Boolean(kyc.data && kyc.data.status !== 'missing'),
    live: status === 'verified',
  };
  const steps = stepStates(done);
  const active = picked ?? firstOpen(steps);
  const go = (d: -1 | 1) => {
    const to = stepFrom(steps, active, d);
    return to ? () => setPicked(to) : undefined;
  };
  const back = go(-1);
  const next = go(1);
  const editable = status === 'draft' || status === 'needs_info';
  const closed = status === 'rejected' || status === 'suspended';

  let body: ReactNode;
  if (!biz.data) {
    body = biz.isError ? <BizError error={biz.error} /> : <Skeleton height="12rem" />;
  } else {
    switch (active) {
      case 'business':
        body = (
          <StepCard id="business" intro={intl.formatMessage({ id: 'onboarding.intro.business' })} onNext={next}>
            <BusinessStep bid={bid} onCreated={noop} />
          </StepCard>
        );
        break;
      case 'store':
        body = (
          <StepCard id="store" intro={intl.formatMessage({ id: 'onboarding.intro.store' })} onBack={back} onNext={first ? next : undefined}>
            {storeList.isLoading && <Skeleton height="4rem" />}
            {storeList.isError && <BizError error={storeList.error} />}
            {storeList.data && !first && <FirstStore bid={bid} onDone={() => setPicked('hours')} />}
            {first && (
              <p className="tz-card-desc">
                {intl.formatMessage({ id: 'onboarding.store.have' }, { name: first.name })}{' '}
                <Link to="/stores">{intl.formatMessage({ id: 'onboarding.store.manage' })}</Link>
              </p>
            )}
          </StepCard>
        );
        break;
      case 'hours':
        body = (
          <StepCard id="hours" intro={intl.formatMessage({ id: 'onboarding.intro.hours' })} onBack={back} onNext={next}>
            {first && settings.data ? <HoursSection bid={bid} sid={first.id} settings={settings.data} /> : <Skeleton height="8rem" />}
          </StepCard>
        );
        break;
      case 'category':
        body = (
          <StepCard id="category" intro={intl.formatMessage({ id: 'onboarding.intro.category' })} onBack={back} onNext={next}>
            <CategoryStep business={biz.data} editable={editable} />
          </StepCard>
        );
        break;
      case 'photos':
        body = (
          <StepCard id="photos" intro={intl.formatMessage({ id: 'onboarding.intro.photos' })} onBack={back} onNext={next}>
            <DocumentsStep bid={bid} editable={!closed} kinds={PHOTO_KINDS} help="onboarding.photos.help" />
          </StepCard>
        );
        break;
      case 'payout':
        body = (
          <StepCard
            id="payout"
            intro={intl.formatMessage({ id: 'onboarding.intro.payout' })}
            onBack={back}
            onNext={next}
            nextLabel={done.payout ? undefined : intl.formatMessage({ id: 'onboarding.skip' })}
          >
            <PayoutSection bid={bid} canEdit={!closed} />
            <h3 className={styles.sub}>{intl.formatMessage({ id: 'onboarding.taxDocs' })}</h3>
            <DocumentsStep bid={bid} editable={!closed} kinds={TAX_KINDS} help="onboarding.taxDocs.help" />
          </StepCard>
        );
        break;
      case 'live':
        body = (
          <StepCard id="live" intro={intl.formatMessage({ id: editable ? 'onboarding.intro.live' : 'onboarding.intro.liveDone' })} onBack={back}>
            <StatusBanner business={biz.data} />
            {editable && <GoLive bid={bid} steps={steps} onFix={setPicked} />}
            {status === 'verified' && (
              <div className={stores.actions}>
                <Link to="/stock" className="tz-btn tz-btn-primary">
                  {intl.formatMessage({ id: 'onboarding.live.addProducts' })}
                </Link>
                <Link to="/" className="tz-btn">
                  {intl.formatMessage({ id: 'onboarding.toDashboard' })}
                </Link>
              </div>
            )}
          </StepCard>
        );
        break;
    }
  }
  return (
    <Frame steps={steps} active={active} onPick={setPicked} home={status === 'verified' || status === 'suspended'}>
      {active !== 'live' && biz.data && status !== 'draft' && <StatusBanner business={biz.data} />}
      {body}
    </Frame>
  );
}

/**
 * The shop's state in its words. Live and unchecked says payouts wait for our check (D-274);
 * the legacy review states stay for businesses that were sent for review before it.
 */
function StatusBanner({ business }: { business: Business }) {
  const intl = useIntl();
  const { status, infoRequest, checkStatus } = business;
  if (status === 'draft') return null;
  const key = status === 'verified' ? (checkStatus === 'checked' ? 'verified_checked' : 'verified') : status;
  const fields = infoRequest?.fields ?? [];
  const tone = status === 'verified' || status === 'submitted' ? 'info' : 'warning';
  return (
    <Banner tone={tone} title={intl.formatMessage({ id: `onboarding.status.${key}.title` })}>
      {intl.formatMessage({ id: `onboarding.status.${key}.body` })}
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
function fieldLabel(intl: IntlShape, path: string): string {
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
  const loaded = bid === null || biz.data;
  if (!loaded) return biz.isError ? <BizError error={biz.error} /> : <Skeleton height="6rem" />;
  const editable = bid === null || biz.data?.status === 'draft' || biz.data?.status === 'needs_info';
  return (
    <BusinessForm
      key={biz.data?.id ?? 'new'}
      initial={{ brandName: biz.data?.brandName ?? '', legalName: biz.data?.legalName ?? '' }}
      editable={editable}
      pending={action.pending}
      error={action.error}
      onSave={async (v) => {
        const out = bid
          ? await action.run('biz-update', async () => {
              const tag = await ifMatch(businessPath(bid), () => getBusiness(bid));
              return updateBusiness(bid, { brandName: v.brandName, legalName: v.legalName || null }, tag ? { ifMatch: tag } : {});
            })
          : await action.run('biz-create', (key) =>
              createBusiness({ brandName: v.brandName, ...(v.legalName ? { legalName: v.legalName } : {}) }, { idempotencyKey: key }),
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
}

function BusinessForm(props: { initial: BusinessValues; editable: boolean; pending: boolean; error: unknown; onSave: (v: BusinessValues) => Promise<void> }) {
  const intl = useIntl();
  const [v, setV] = useState(props.initial);
  const [shown, setShown] = useState(false);
  const nameMissing = v.brandName.trim() === '';
  return (
    <form
      className={stores.stack}
      noValidate
      onSubmit={(e) => {
        e.preventDefault();
        setShown(true);
        if (!nameMissing) void props.onSave({ brandName: v.brandName.trim(), legalName: v.legalName.trim() });
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
      <BizError error={props.error} />
      {props.editable && (
        <div className={stores.actions}>
          <Button type="submit" variant="primary" loading={props.pending}>
            {intl.formatMessage({ id: 'onboarding.save' })}
          </Button>
        </div>
      )}
    </form>
  );
}

/** Category tiles: one tap saves (If-Match on the business). Read-only once the shop is live. */
function CategoryStep({ business, editable }: { business: Business; editable: boolean }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const action = useAction();
  const ifMatch = useIfMatch();
  const cats = useCategories();
  const pick = async (categoryId: string) => {
    if (categoryId === business.categoryId) return;
    const bid = business.id;
    const out = await action.run('biz-category', async () => {
      const tag = await ifMatch(businessPath(bid), () => getBusiness(bid));
      return updateBusiness(bid, { categoryId }, tag ? { ifMatch: tag } : {});
    });
    if (!out) return;
    await qc.invalidateQueries({ queryKey: bizKey(bid) });
    toast.show({ message: intl.formatMessage({ id: 'onboarding.saved' }) });
  };
  if (cats.isError) return <BizError error={cats.error} />;
  if (!cats.data) return <Skeleton height="8rem" />;
  return (
    <fieldset className={styles.tiles} disabled={!editable || action.pending}>
      <legend className={styles.legend}>{intl.formatMessage({ id: 'onboarding.business.category' })}</legend>
      {cats.data.items.map((c) => (
        <label key={c.id} className={styles.tile}>
          <input type="radio" name="onb-category" value={c.id} checked={business.categoryId === c.id} onChange={() => void pick(c.id)} />
          <span>{c.name}</span>
          {business.categoryId === c.id && <Check size={16} aria-hidden="true" className={styles.tileTick} />}
        </label>
      ))}
      <BizError error={action.error} />
    </fieldset>
  );
}

function FirstStore({ bid, onDone }: { bid: string; onDone: () => void }) {
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
    onDone();
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

/** The last step: what is still missing, then Go live (submit). The shop is live the moment it answers. */
function GoLive({ bid, steps, onFix }: { bid: string; steps: StepState[]; onFix: (id: StepId) => void }) {
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
  const todo = steps.filter((s) => s.id !== 'live' && !s.done && !OPTIONAL.has(s.id));
  return (
    <div className={stores.stack}>
      {todo.length > 0 && (
        <ul className={styles.todo}>
          {todo.map((s) => (
            <li key={s.id}>
              <button type="button" className={styles.todoLink} disabled={!s.reachable} onClick={() => onFix(s.id)}>
                {stepName(intl, s.id)}
                {s.id === 'photos' && ` · ${intl.formatMessage({ id: 'onboarding.recommended' })}`}
              </button>
            </li>
          ))}
        </ul>
      )}
      <p className={styles.note}>{intl.formatMessage({ id: 'onboarding.submit.body' })}</p>
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
      <div className={stores.actions}>
        <Button variant="primary" loading={action.pending} onClick={() => void send()}>
          {intl.formatMessage({ id: 'onboarding.submit.action' })}
        </Button>
      </div>
    </div>
  );
}
