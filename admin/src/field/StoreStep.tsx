import { ApiError } from '@todayz/api-client';
import type { FieldBusiness } from '@todayz/api-client/gen/model/fieldBusiness';
import type { FieldStoreStatus } from '@todayz/api-client/gen/model/fieldStoreStatus';
import type { FieldStoreWeek } from '@todayz/api-client/gen/model/fieldStoreWeek';
import { Button, StatusPill, TextField } from '@todayz/ui';
import { LocateFixed } from 'lucide-react';
import { useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { useProblemText } from '../app/problems';
import { toE164 } from './labels';
import { useCreateStore } from './queries';
import css from './field.module.css';

const DAYS = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] as const;
type Day = (typeof DAYS)[number];
const DAY: Record<Day, string> = {
  mon: 'field.day.mon',
  tue: 'field.day.tue',
  wed: 'field.day.wed',
  thu: 'field.day.thu',
  fri: 'field.day.fri',
  sat: 'field.day.sat',
  sun: 'field.day.sun',
};
const STORE_STATUS: Record<FieldStoreStatus, string> = {
  draft: 'field.store.status.draft',
  pending_review: 'field.store.status.pendingReview',
  live: 'field.store.status.live',
  paused: 'field.store.status.paused',
  suspended: 'field.store.status.suspended',
};
const TIME = /^([01][0-9]|2[0-3]):[0-5][0-9]$/;

interface Values {
  name: string;
  addressLine1: string;
  addressLine2: string;
  landmark: string;
  locality: string;
  city: string;
  pincode: string;
  lat: string;
  lng: string;
  phone: string;
  open: string;
  close: string;
  days: Record<Day, boolean>;
}

const EMPTY: Values = {
  name: '',
  addressLine1: '',
  addressLine2: '',
  landmark: '',
  locality: '',
  city: '',
  pincode: '',
  lat: '',
  lng: '',
  phone: '',
  open: '09:00',
  close: '21:00',
  days: { mon: true, tue: true, wed: true, thu: true, fri: true, sat: true, sun: false },
};

/** A pin inside India's box (the contract's bounds), or null. */
function pin(v: Values): { lat: number; lng: number } | null {
  const lat = Number(v.lat);
  const lng = Number(v.lng);
  if (!v.lat.trim() || !v.lng.trim() || !Number.isFinite(lat) || !Number.isFinite(lng)) return null;
  return lat >= 6 && lat <= 38 && lng >= 68 && lng <= 98 ? { lat, lng } : null;
}

/** Step 2: the first store with everything the review needs: address, pin, phone, hours. */
export function StoreStep({ business: b, onDone }: { business: FieldBusiness; onDone: () => void }) {
  const intl = useIntl();
  const [adding, setAdding] = useState(b.stores.length === 0);
  return (
    <div className={css.form}>
      {b.stores.length > 0 && (
        <ul className={css.list}>
          {b.stores.map((s) => (
            <li key={s.id} className={css.shop}>
              <span>
                <span className={css.shopName}>{s.name}</span>
                <br />
                <span className={css.muted}>{s.locality ?? '—'}</span>
              </span>
              <StatusPill tone={s.status === 'live' ? 'completed' : s.status === 'pending_review' ? 'pending' : 'neutral'}>
                {intl.formatMessage({ id: STORE_STATUS[s.status] })}
              </StatusPill>
            </li>
          ))}
        </ul>
      )}
      {!b.itemsEditable ? (
        <p className={css.muted}>{intl.formatMessage({ id: 'field.readOnly' })}</p>
      ) : adding ? (
        <StoreForm bid={b.id} onDone={onDone} />
      ) : (
        <div className={css.head}>
          <Button variant="soft" onClick={() => setAdding(true)}>
            {intl.formatMessage({ id: 'field.store.addAnother' })}
          </Button>
          <Button variant="primary" onClick={onDone}>
            {intl.formatMessage({ id: 'field.next' })}
          </Button>
        </div>
      )}
    </div>
  );
}

function StoreForm({ bid, onDone }: { bid: string; onDone: () => void }) {
  const intl = useIntl();
  const problemText = useProblemText();
  const create = useCreateStore(bid);
  const [v, setV] = useState<Values>(EMPTY);
  const [shown, setShown] = useState(false);
  const [locating, setLocating] = useState<'idle' | 'busy' | 'failed'>('idle');
  const set = (k: keyof Omit<Values, 'days'>) => (e: React.ChangeEvent<HTMLInputElement>) => setV((p) => ({ ...p, [k]: e.target.value }));
  const t = (id: string) => intl.formatMessage({ id });

  const phone = toE164(v.phone);
  const where = pin(v);
  const errors: Partial<Record<keyof Values, string>> = {
    ...(v.name.trim() ? {} : { name: t('field.store.required') }),
    ...(v.addressLine1.trim() ? {} : { addressLine1: t('field.store.required') }),
    ...(v.locality.trim() ? {} : { locality: t('field.store.required') }),
    ...(v.city.trim() ? {} : { city: t('field.store.required') }),
    ...(/^[1-9][0-9]{5}$/.test(v.pincode.trim()) ? {} : { pincode: t('field.store.pincode.invalid') }),
    ...(where ? {} : { lat: t('field.store.pin.invalid') }),
    ...(phone ? {} : { phone: t('field.new.phone.invalid') }),
    ...(TIME.test(v.open) && TIME.test(v.close) ? {} : { open: t('field.store.hours.invalid') }),
    ...(DAYS.some((d) => v.days[d]) ? {} : { days: t('field.store.days.required') }),
  };
  const err = (k: keyof Values) => (shown ? errors[k] : undefined);

  const locate = () => {
    if (!('geolocation' in navigator)) {
      setLocating('failed');
      return;
    }
    setLocating('busy');
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        setV((p) => ({ ...p, lat: pos.coords.latitude.toFixed(6), lng: pos.coords.longitude.toFixed(6) }));
        setLocating('idle');
      },
      () => setLocating('failed'),
      { enableHighAccuracy: true, timeout: 10_000 },
    );
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setShown(true);
    if (Object.keys(errors).length > 0 || !where || !phone) return;
    const weekly: FieldStoreWeek = {};
    for (const d of DAYS) weekly[d] = v.days[d] ? [{ open: v.open, close: v.close }] : [];
    const opt = (k: 'addressLine2' | 'landmark') => (v[k].trim() ? { [k]: v[k].trim() } : {});
    create.mutate(
      {
        name: v.name.trim(),
        addressLine1: v.addressLine1.trim(),
        ...opt('addressLine2'),
        ...opt('landmark'),
        locality: v.locality.trim(),
        city: v.city.trim(),
        pincode: v.pincode.trim(),
        location: where,
        phone,
        hours: { weekly },
      },
      { onSuccess: onDone },
    );
  };

  return (
    <form className={css.form} onSubmit={submit} noValidate aria-label={t('field.store.form')}>
      <TextField label={t('field.store.name')} value={v.name} maxLength={80} onChange={set('name')} error={err('name')} />
      <TextField label={t('field.store.line1')} value={v.addressLine1} maxLength={200} onChange={set('addressLine1')} error={err('addressLine1')} />
      <TextField label={t('field.store.line2')} value={v.addressLine2} maxLength={200} onChange={set('addressLine2')} />
      <TextField label={t('field.store.landmark')} value={v.landmark} maxLength={120} onChange={set('landmark')} />
      <div className={css.row}>
        <TextField label={t('field.store.locality')} value={v.locality} maxLength={80} onChange={set('locality')} error={err('locality')} />
        <TextField label={t('field.store.city')} value={v.city} maxLength={80} onChange={set('city')} error={err('city')} />
      </div>
      <TextField
        label={t('field.store.pincode')}
        inputMode="numeric"
        maxLength={6}
        value={v.pincode}
        onChange={set('pincode')}
        error={err('pincode')}
      />
      <div className={css.form}>
        <Button type="button" variant="soft" icon={LocateFixed} loading={locating === 'busy'} onClick={locate}>
          {t('field.store.locate')}
        </Button>
        {locating === 'failed' && <p className={css.muted}>{t('field.store.locate.failed')}</p>}
        <div className={css.row}>
          <TextField label={t('field.store.lat')} inputMode="decimal" value={v.lat} onChange={set('lat')} error={err('lat')} />
          <TextField label={t('field.store.lng')} inputMode="decimal" value={v.lng} onChange={set('lng')} />
        </div>
      </div>
      <TextField
        label={t('field.store.phone')}
        prefix="+91"
        type="tel"
        inputMode="numeric"
        autoComplete="off"
        maxLength={14}
        value={v.phone}
        onChange={set('phone')}
        error={err('phone')}
      />
      <fieldset className={css.form}>
        <legend className="tz-field-label">{t('field.store.hours')}</legend>
        <div className={css.row}>
          <TextField label={t('field.store.open')} type="time" value={v.open} onChange={set('open')} error={err('open')} />
          <TextField label={t('field.store.close')} type="time" value={v.close} onChange={set('close')} />
        </div>
        <div className={css.photos} role="group" aria-label={t('field.store.days')}>
          {DAYS.map((d) => (
            <label key={d} className={css.stepTab}>
              <input type="checkbox" checked={v.days[d]} onChange={(e) => setV((p) => ({ ...p, days: { ...p.days, [d]: e.target.checked } }))} />
              {t(DAY[d])}
            </label>
          ))}
        </div>
        {err('days') && <p className="tz-field-error">{err('days')}</p>}
      </fieldset>
      {create.isError && (
        <p role="alert" className="tz-field-error">
          {create.error instanceof ApiError && Object.keys(create.error.fields).length > 0 ? t('common.error.invalid') : problemText(create.error)}
        </p>
      )}
      <Button type="submit" variant="primary" block loading={create.isPending}>
        {t('field.store.save')}
      </Button>
    </form>
  );
}
