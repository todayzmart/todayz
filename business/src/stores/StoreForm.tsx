import type { Store } from '@todayz/api-client/gen/model/store';
import { Button, TextField } from '@todayz/ui';
import { LocateFixed } from 'lucide-react';
import { useMemo, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { BizError } from './BizError';
import { validateStore, valuesOf, type StoreValues } from './storeValues';
import styles from './stores.module.css';

export interface StoreFormProps {
  /** The store being edited; none for a new one. */
  store?: Store | undefined;
  submitLabel: string;
  pending: boolean;
  error: unknown;
  /** Only called with values that passed the local checks. */
  onSubmit: (values: StoreValues, before: StoreValues) => void;
  /** Extra button beside Save (e.g. Cancel in a sheet). */
  secondary?: React.ReactNode;
}

/** Name, address, map pin and phone of one store (B-02 store step, B-13 add, B-14 profile). */
export function StoreForm({ store, submitLabel, pending, error, onSubmit, secondary }: StoreFormProps) {
  const intl = useIntl();
  const before = useMemo(() => valuesOf(store), [store]);
  const [v, setV] = useState<StoreValues>(before);
  const [shown, setShown] = useState(false);
  const [locating, setLocating] = useState<'idle' | 'busy' | 'failed'>('idle');
  const errors = validateStore(v);
  const err = (k: keyof StoreValues) => (shown && errors[k] ? intl.formatMessage({ id: `stores.form.err.${errors[k]}` }) : undefined);
  const set = (k: keyof StoreValues) => (e: React.ChangeEvent<HTMLInputElement>) => setV((p) => ({ ...p, [k]: e.target.value }));

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
    if (Object.keys(errors).length === 0) onSubmit(v, before);
  };

  return (
    <form className={styles.stack} onSubmit={submit} noValidate>
      <TextField label={intl.formatMessage({ id: 'stores.form.name' })} value={v.name} maxLength={80} onChange={set('name')} error={err('name')} autoComplete="off" />
      <TextField label={intl.formatMessage({ id: 'stores.form.address1' })} value={v.addressLine1} maxLength={200} onChange={set('addressLine1')} autoComplete="off" />
      <TextField label={intl.formatMessage({ id: 'stores.form.address2' })} value={v.addressLine2} maxLength={200} onChange={set('addressLine2')} autoComplete="off" />
      <div className={styles.grid}>
        <TextField label={intl.formatMessage({ id: 'stores.form.landmark' })} value={v.landmark} maxLength={120} onChange={set('landmark')} autoComplete="off" />
        <TextField label={intl.formatMessage({ id: 'stores.form.locality' })} value={v.locality} maxLength={80} onChange={set('locality')} autoComplete="off" />
        <TextField label={intl.formatMessage({ id: 'stores.form.city' })} value={v.city} maxLength={80} onChange={set('city')} autoComplete="off" />
        <TextField
          label={intl.formatMessage({ id: 'stores.form.pincode' })}
          value={v.pincode}
          inputMode="numeric"
          maxLength={6}
          onChange={set('pincode')}
          error={err('pincode')}
          autoComplete="off"
        />
      </div>
      <TextField
        label={intl.formatMessage({ id: 'stores.form.phone' })}
        help={intl.formatMessage({ id: 'stores.form.phoneHelp' })}
        prefix="+91"
        value={v.phone}
        inputMode="tel"
        maxLength={10}
        onChange={set('phone')}
        error={err('phone')}
        autoComplete="off"
      />
      <fieldset className={styles.fieldset}>
        <legend className={styles.legend}>{intl.formatMessage({ id: 'stores.form.pin' })}</legend>
        <p className={styles.note}>{intl.formatMessage({ id: 'stores.form.pinHelp' })}</p>
        <div className={styles.grid}>
          <TextField label={intl.formatMessage({ id: 'stores.form.lat' })} value={v.lat} inputMode="decimal" onChange={set('lat')} error={err('lat')} autoComplete="off" />
          <TextField label={intl.formatMessage({ id: 'stores.form.lng' })} value={v.lng} inputMode="decimal" onChange={set('lng')} error={err('lng')} autoComplete="off" />
        </div>
        <div className={styles.actions}>
          <Button icon={LocateFixed} loading={locating === 'busy'} onClick={locate}>
            {intl.formatMessage({ id: 'stores.form.locate' })}
          </Button>
        </div>
        {locating === 'failed' && (
          <p role="alert" className="tz-field-error">
            {intl.formatMessage({ id: 'stores.form.locateFailed' })}
          </p>
        )}
      </fieldset>
      <BizError error={error} />
      <div className={styles.actions}>
        <Button type="submit" variant="primary" loading={pending}>
          {submitLabel}
        </Button>
        {secondary}
      </div>
    </form>
  );
}
