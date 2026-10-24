import type { StoreHours } from '@todayz/api-client/gen/model/storeHours';
import type { StoreSettings } from '@todayz/api-client/gen/model/storeSettings';
import { getStore, putKyc, updateStore } from '@todayz/api-client/gen/business/business';
import type { Store } from '@todayz/api-client/gen/model/store';
import { Banner, Button, TextField, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { EMPTY_KYC, kycPayload, validateKyc, type KycValues } from '../onboarding/kyc';
import { BizError } from '../stores/BizError';
import { HoursEditor } from '../stores/HoursEditor';
import { hoursPayload, hoursValid, sameHours, validateHours } from '../stores/hours';
import { bizKey, kycKey, storePath, storesKey, useAction, useIfMatch, useKyc } from '../stores/queries';
import { StoreForm } from '../stores/StoreForm';
import { updatePayload, type StoreValues } from '../stores/storeValues';
import styles from '../stores/stores.module.css';
import {
  CASHBACK_OPTIONS,
  cashbackCredits,
  deliveryPayload,
  deliveryValuesOf,
  inStoreOnly,
  validateDelivery,
  type DeliveryValues,
} from './delivery';
import { todayIst } from './today';
import { useSettingsSave } from './useSettingsSave';

interface Ids {
  bid: string;
  sid: string;
}

export function ProfileSection({ bid, store }: { bid: string; store: Store }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const action = useAction();
  const ifMatch = useIfMatch();
  const save = async (v: StoreValues, before: StoreValues) => {
    const body = updatePayload(v, before);
    if (Object.keys(body).length === 0) return;
    const out = await action.run(`profile-${store.id}`, async () => {
      const tag = await ifMatch(storePath(bid, store.id), () => getStore(bid, store.id));
      return updateStore(bid, store.id, body, tag ? { ifMatch: tag } : {});
    });
    if (!out) return;
    await qc.invalidateQueries({ queryKey: storesKey(bid) });
    await qc.invalidateQueries({ queryKey: bizKey(bid) });
    toast.show({ message: intl.formatMessage({ id: 'settings.saved' }) });
  };
  return (
    <section className="tz-card" aria-labelledby="set-profile">
      <h2 id="set-profile" className="tz-card-title">
        {intl.formatMessage({ id: 'settings.profile.title' })}
      </h2>
      <p className="tz-card-desc">{intl.formatMessage({ id: 'settings.profile.body' })}</p>
      <StoreForm
        key={store.id}
        store={store}
        submitLabel={intl.formatMessage({ id: 'settings.save' })}
        pending={action.pending}
        error={action.error}
        onSubmit={(v, before) => void save(v, before)}
      />
    </section>
  );
}

export function HoursSection({ bid, sid, settings }: Ids & { settings: StoreSettings }) {
  const intl = useIntl();
  const toast = useToast();
  const s = useSettingsSave(bid, sid);
  const [hours, setHours] = useState<StoreHours>(settings.hours);
  const [shown, setShown] = useState(false);
  const errors = validateHours(hours);
  const submit = async () => {
    setShown(true);
    if (!hoursValid(errors) || sameHours(hours, settings.hours)) return;
    if (await s.save('hours', { hours: hoursPayload(hours) })) toast.show({ message: intl.formatMessage({ id: 'settings.saved' }) });
  };
  return (
    <section className="tz-card" aria-labelledby="set-hours">
      <h2 id="set-hours" className="tz-card-title">
        {intl.formatMessage({ id: 'settings.hours.title' })}
      </h2>
      <p className="tz-card-desc">{intl.formatMessage({ id: 'settings.hours.body' })}</p>
      <HoursEditor value={hours} onChange={setHours} errors={errors} showErrors={shown} minDate={todayIst()} />
      <BizError error={s.error} />
      <div className={styles.actions}>
        <Button variant="primary" loading={s.pending} onClick={() => void submit()}>
          {intl.formatMessage({ id: 'settings.save' })}
        </Button>
      </div>
    </section>
  );
}

export function DeliverySection({ bid, sid, settings }: Ids & { settings: StoreSettings }) {
  const intl = useIntl();
  const toast = useToast();
  const s = useSettingsSave(bid, sid);
  const before = deliveryValuesOf(settings);
  const [v, setV] = useState<DeliveryValues>(before);
  const [shown, setShown] = useState(false);
  const errors = validateDelivery(v);
  const text = (k: 'radiusKm' | 'feeRupees' | 'freeAboveRupees' | 'minOrderRupees') => (e: React.ChangeEvent<HTMLInputElement>) =>
    setV((p) => ({ ...p, [k]: e.target.value }));
  const err = (k: keyof typeof errors) => (shown && errors[k] ? intl.formatMessage({ id: `settings.delivery.err.${errors[k]}` }) : undefined);
  const submit = async () => {
    setShown(true);
    if (Object.keys(errors).length > 0) return;
    if (await s.save('delivery', deliveryPayload(v, before))) toast.show({ message: intl.formatMessage({ id: 'settings.saved' }) });
  };
  return (
    <section className="tz-card" aria-labelledby="set-delivery">
      <h2 id="set-delivery" className="tz-card-title">
        {intl.formatMessage({ id: 'settings.delivery.title' })}
      </h2>
      <label className={styles.check}>
        <input type="checkbox" checked={v.pickup} onChange={(e) => setV((p) => ({ ...p, pickup: e.target.checked }))} />
        <span>{intl.formatMessage({ id: 'settings.delivery.pickup' })}</span>
      </label>
      <label className={styles.check}>
        <input type="checkbox" checked={v.delivery} onChange={(e) => setV((p) => ({ ...p, delivery: e.target.checked }))} />
        <span>{intl.formatMessage({ id: 'settings.delivery.delivery' })}</span>
      </label>
      {inStoreOnly(v) && (
        <Banner tone="warning" title={intl.formatMessage({ id: 'settings.delivery.inStoreOnly.title' })}>
          {intl.formatMessage({ id: 'settings.delivery.inStoreOnly.body' })}
        </Banner>
      )}
      {v.delivery && (
        <div className={styles.grid}>
          <TextField
            label={intl.formatMessage({ id: 'settings.delivery.radius' })}
            help={intl.formatMessage({ id: 'settings.delivery.radiusHelp' })}
            inputMode="decimal"
            value={v.radiusKm}
            onChange={text('radiusKm')}
            error={err('radiusKm')}
          />
          <TextField label={intl.formatMessage({ id: 'settings.delivery.fee' })} prefix="₹" inputMode="decimal" value={v.feeRupees} onChange={text('feeRupees')} error={err('feeRupees')} />
          <TextField
            label={intl.formatMessage({ id: 'settings.delivery.freeAbove' })}
            help={intl.formatMessage({ id: 'settings.delivery.freeAboveHelp' })}
            prefix="₹"
            inputMode="decimal"
            value={v.freeAboveRupees}
            onChange={text('freeAboveRupees')}
            error={err('freeAboveRupees')}
          />
          <TextField label={intl.formatMessage({ id: 'settings.delivery.minOrder' })} prefix="₹" inputMode="decimal" value={v.minOrderRupees} onChange={text('minOrderRupees')} error={err('minOrderRupees')} />
        </div>
      )}
      <BizError error={s.error} />
      <div className={styles.actions}>
        <Button variant="primary" loading={s.pending} onClick={() => void submit()}>
          {intl.formatMessage({ id: 'settings.save' })}
        </Button>
      </div>
    </section>
  );
}

const EXAMPLE_BILL_PAISE = 100_000;

export function RewardsSection({ bid, sid, settings, canMuteAsks }: Ids & { settings: StoreSettings; canMuteAsks: boolean }) {
  const intl = useIntl();
  const toast = useToast();
  const s = useSettingsSave(bid, sid);
  const [bp, setBp] = useState(settings.cashbackBp);
  const [auto, setAuto] = useState(settings.autoAccept);
  const [muted, setMuted] = useState(settings.asksMuted);
  const submit = async () => {
    const body: Parameters<typeof s.save>[1] = {};
    if (bp !== settings.cashbackBp) body.cashbackBp = bp;
    if (auto !== settings.autoAccept) body.autoAccept = auto;
    if (muted !== settings.asksMuted) body.asksMuted = muted;
    if (await s.save('rewards', body)) toast.show({ message: intl.formatMessage({ id: 'settings.saved' }) });
  };
  return (
    <section className="tz-card" aria-labelledby="set-rewards">
      <h2 id="set-rewards" className="tz-card-title">
        {intl.formatMessage({ id: 'settings.rewards.title' })}
      </h2>
      <div className="tz-field">
        <label className="tz-field-label" htmlFor="set-cashback">
          {intl.formatMessage({ id: 'settings.rewards.cashback' })}
        </label>
        <select id="set-cashback" className={`tz-input ${styles.select}`} value={bp} onChange={(e) => setBp(Number(e.target.value))}>
          {CASHBACK_OPTIONS.map((o) => (
            <option key={o} value={o}>
              {intl.formatNumber(o / 10_000, { style: 'percent', maximumFractionDigits: 1 })}
            </option>
          ))}
        </select>
        <span className="tz-field-help">
          {intl.formatMessage(
            { id: 'settings.rewards.example' },
            { amount: intl.formatNumber(EXAMPLE_BILL_PAISE / 100, { style: 'currency', currency: 'INR', maximumFractionDigits: 0 }), credits: cashbackCredits(EXAMPLE_BILL_PAISE, bp) },
          )}
        </span>
      </div>
      <label className={styles.check}>
        <input type="checkbox" checked={auto} onChange={(e) => setAuto(e.target.checked)} />
        <span>{intl.formatMessage({ id: 'settings.rewards.autoAccept' })}</span>
      </label>
      {canMuteAsks && (
        <label className={styles.check}>
          <input type="checkbox" checked={!muted} onChange={(e) => setMuted(!e.target.checked)} />
          <span>{intl.formatMessage({ id: 'settings.rewards.asks' })}</span>
        </label>
      )}
      <BizError error={s.error} />
      <div className={styles.actions}>
        <Button variant="primary" loading={s.pending} onClick={() => void submit()}>
          {intl.formatMessage({ id: 'settings.save' })}
        </Button>
      </div>
    </section>
  );
}

/** Payout and tax details: the owner edits (blank keeps what is saved); a manager sees them masked. */
export function PayoutSection({ bid, canEdit }: { bid: string; canEdit: boolean }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const kyc = useKyc(bid);
  const action = useAction();
  const [v, setV] = useState<KycValues>(EMPTY_KYC);
  const [shown, setShown] = useState(false);
  const have = { bank: Boolean(kyc.data?.bankAccountLast4) };
  const errors = validateKyc(v, have);
  const set = (k: keyof KycValues) => (e: React.ChangeEvent<HTMLInputElement>) => setV((p) => ({ ...p, [k]: e.target.value }));
  const err = (k: keyof KycValues) => (shown && errors[k] ? intl.formatMessage({ id: `settings.payout.err.${errors[k]}` }) : undefined);
  const saved = (last4: string | null | undefined) =>
    last4 ? intl.formatMessage({ id: 'settings.payout.savedEnding' }, { last4 }) : intl.formatMessage({ id: 'settings.payout.notSet' });
  const submit = async () => {
    setShown(true);
    const body = kycPayload(v);
    if (Object.keys(errors).length > 0 || Object.keys(body).length === 0) return;
    const out = await action.run('kyc', async () => putKyc(bid, body));
    if (!out) return;
    setV(EMPTY_KYC);
    setShown(false);
    await qc.invalidateQueries({ queryKey: kycKey(bid) });
    toast.show({ message: intl.formatMessage({ id: 'settings.saved' }) });
  };
  return (
    <section className="tz-card" aria-labelledby="set-payout">
      <h2 id="set-payout" className="tz-card-title">
        {intl.formatMessage({ id: 'settings.payout.title' })}
      </h2>
      {!canEdit && <p className="tz-card-desc">{intl.formatMessage({ id: 'settings.payout.managerNote' })}</p>}
      {kyc.isError && <BizError error={kyc.error} />}
      {kyc.data && (
        <dl className={styles.stack}>
          <div>
            <dt className={styles.meta}>{intl.formatMessage({ id: 'settings.payout.pan' })}</dt>
            <dd>{saved(kyc.data.panLast4)}</dd>
          </div>
          <div>
            <dt className={styles.meta}>{intl.formatMessage({ id: 'settings.payout.gstin' })}</dt>
            <dd>{saved(kyc.data.gstinLast4)}</dd>
          </div>
          <div>
            <dt className={styles.meta}>{intl.formatMessage({ id: 'settings.payout.upi' })}</dt>
            <dd>{saved(kyc.data.upiIdLast4)}</dd>
          </div>
          <div>
            <dt className={styles.meta}>{intl.formatMessage({ id: 'settings.payout.bank' })}</dt>
            <dd>{saved(kyc.data.bankAccountLast4)}{kyc.data.bankIfsc ? ` · ${kyc.data.bankIfsc}` : ''}</dd>
          </div>
        </dl>
      )}
      {canEdit && kyc.data && (
        <form
          className={styles.stack}
          noValidate
          onSubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          <p className={styles.note}>{intl.formatMessage({ id: 'settings.payout.replaceHelp' })}</p>
          <TextField label={intl.formatMessage({ id: 'settings.payout.legalName' })} value={v.legalName} onChange={set('legalName')} error={err('legalName')} autoComplete="off" />
          <TextField label={intl.formatMessage({ id: 'settings.payout.pan' })} value={v.pan} onChange={set('pan')} error={err('pan')} maxLength={10} autoComplete="off" />
          <TextField label={intl.formatMessage({ id: 'settings.payout.gstin' })} value={v.gstin} onChange={set('gstin')} error={err('gstin')} maxLength={15} autoComplete="off" />
          <TextField label={intl.formatMessage({ id: 'settings.payout.upi' })} value={v.upiId} onChange={set('upiId')} error={err('upiId')} autoComplete="off" />
          <TextField label={intl.formatMessage({ id: 'settings.payout.bankAccount' })} inputMode="numeric" value={v.bankAccount} onChange={set('bankAccount')} error={err('bankAccount')} autoComplete="off" />
          <TextField label={intl.formatMessage({ id: 'settings.payout.ifsc' })} value={v.bankIfsc} onChange={set('bankIfsc')} error={err('bankIfsc')} maxLength={11} autoComplete="off" />
          <BizError error={action.error} />
          <div className={styles.actions}>
            <Button type="submit" variant="primary" loading={action.pending}>
              {intl.formatMessage({ id: 'settings.save' })}
            </Button>
          </div>
        </form>
      )}
    </section>
  );
}
