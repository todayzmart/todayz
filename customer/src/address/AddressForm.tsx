// Add an address (C-11, C-18): the lines the rider reads and a pin the delivery radius is checked
// against (01 §5.5). The pin starts at the place the app already knows (C-02) and moves to the
// device's location on request. Nothing typed here is logged or kept outside the form.
import { ApiError } from '@todayz/api-client';
import type { Address } from '@todayz/api-client/gen/model/address';
import type { DiscoverStorePage } from '@todayz/api-client/gen/model/discoverStorePage';
import { formatDistance } from '@todayz/i18n';
import { Banner, Button, TextField } from '@todayz/ui';
import { LocateFixed } from 'lucide-react';
import { lazy, Suspense, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { inRadius } from '../cart/fulfilment';
import styles from '../cart/tx.module.css';
import { useCoords } from '../home/coords';
import { Icon } from '../shell/Icon';
import { useAddressActions } from './queries';

const PinMap = lazy(() => import('./PinMap'));

type Pin = { lat: number; lng: number; source: 'device' | 'area' };
type Field = 'label' | 'line1' | 'line2' | 'landmark' | 'pincode' | 'pin';

const PINCODE = /^[1-9][0-9]{5}$/;

export function AddressForm({
  store,
  onSaved,
  onCancel,
}: {
  /** At checkout: the shop, to draw the radius and say whether the pin is inside it. */
  store?: DiscoverStorePage | undefined;
  onSaved: (a: Address) => void;
  onCancel: () => void;
}) {
  const intl = useIntl();
  const area = useCoords();
  const { create } = useAddressActions();
  const [label, setLabel] = useState(intl.formatMessage({ id: 'address.form.labelDefault' }));
  const [line1, setLine1] = useState('');
  const [line2, setLine2] = useState('');
  const [landmark, setLandmark] = useState('');
  const [pincode, setPincode] = useState('');
  const [devicePin, setDevicePin] = useState<Pin | null>(null);
  const [locating, setLocating] = useState(false);
  const [locateFailed, setLocateFailed] = useState(false);
  const [errors, setErrors] = useState<Partial<Record<Field, string>>>({});
  const [failed, setFailed] = useState(false);

  const pin: Pin | null = devicePin ?? (area.status === 'ready' ? { ...area.coords, source: 'area' } : null);
  const fits = store && pin ? inRadius(store, pin) : null;

  const locate = () => {
    if (!('geolocation' in navigator)) {
      setLocateFailed(true);
      return;
    }
    setLocating(true);
    setLocateFailed(false);
    navigator.geolocation.getCurrentPosition(
      (p) => {
        setDevicePin({ lat: p.coords.latitude, lng: p.coords.longitude, source: 'device' });
        setLocating(false);
      },
      () => {
        setLocateFailed(true);
        setLocating(false);
      },
      { enableHighAccuracy: true, timeout: 15_000 },
    );
  };

  const required = intl.formatMessage({ id: 'address.form.required' });
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const next: Partial<Record<Field, string>> = {};
    if (!label.trim()) next.label = required;
    if (!line1.trim()) next.line1 = required;
    if (!PINCODE.test(pincode.trim())) next.pincode = intl.formatMessage({ id: 'address.form.pincodeError' });
    if (!pin) next.pin = intl.formatMessage({ id: 'address.form.pinError' });
    setErrors(next);
    setFailed(false);
    if (Object.keys(next).length > 0 || !pin) return;
    try {
      const saved = await create.mutateAsync({
        label: label.trim(),
        line1: line1.trim(),
        pincode: pincode.trim(),
        lat: pin.lat,
        lng: pin.lng,
        ...(line2.trim() ? { line2: line2.trim() } : {}),
        ...(landmark.trim() ? { landmark: landmark.trim() } : {}),
      });
      onSaved(saved);
    } catch (err) {
      const fields = err instanceof ApiError ? (err.problem.fields ?? {}) : {};
      const byField: Partial<Record<Field, string>> = {};
      for (const f of ['label', 'line1', 'line2', 'landmark', 'pincode'] as const) {
        if (fields[`body.${f}`]) byField[f] = intl.formatMessage({ id: 'address.form.invalid' });
      }
      if (Object.keys(byField).length > 0) setErrors(byField);
      else setFailed(true);
    }
  };

  return (
    <form className={styles.stack} onSubmit={(e) => void submit(e)} noValidate aria-label={intl.formatMessage({ id: 'address.form.title' })}>
      <h3 className="tz-card-title">{intl.formatMessage({ id: 'address.form.title' })}</h3>
      {failed && (
        <Banner tone="warning" live="status">
          {intl.formatMessage({ id: 'address.form.failed' })}
        </Banner>
      )}
      <TextField label={intl.formatMessage({ id: 'address.form.label' })} value={label} maxLength={40} onChange={(e) => setLabel(e.target.value)} error={errors.label} />
      <TextField
        label={intl.formatMessage({ id: 'address.form.line1' })}
        help={intl.formatMessage({ id: 'address.form.line1Help' })}
        value={line1}
        maxLength={200}
        autoComplete="address-line1"
        onChange={(e) => setLine1(e.target.value)}
        error={errors.line1}
      />
      <TextField
        label={intl.formatMessage({ id: 'address.form.line2' })}
        value={line2}
        maxLength={200}
        autoComplete="address-line2"
        onChange={(e) => setLine2(e.target.value)}
        error={errors.line2}
      />
      <TextField label={intl.formatMessage({ id: 'address.form.landmark' })} value={landmark} maxLength={120} onChange={(e) => setLandmark(e.target.value)} error={errors.landmark} />
      <TextField
        label={intl.formatMessage({ id: 'address.form.pincode' })}
        value={pincode}
        inputMode="numeric"
        maxLength={6}
        autoComplete="postal-code"
        onChange={(e) => setPincode(e.target.value.replace(/\D/g, ''))}
        error={errors.pincode}
      />

      <div className={styles.stack}>
        <p className={styles.name}>{intl.formatMessage({ id: 'address.form.pinTitle' })}</p>
        <p className={styles.meta} role="status">
          {pin
            ? intl.formatMessage({ id: pin.source === 'device' ? 'address.form.pinDevice' : 'address.form.pinArea' })
            : intl.formatMessage({ id: 'address.form.pinNone' })}
          {fits !== null && store && (
            <>
              {' '}
              {intl.formatMessage(
                { id: fits ? 'address.form.inside' : 'address.form.outside' },
                { distance: formatDistance(store.deliveryRadiusM) },
              )}
            </>
          )}
        </p>
        {errors.pin && <p className={styles.flag}>{errors.pin}</p>}
        {locateFailed && <p className={styles.flag}>{intl.formatMessage({ id: 'address.form.locateFailed' })}</p>}
        {store && pin && (
          <Suspense fallback={null}>
            <PinMap pin={pin} store={store} />
          </Suspense>
        )}
        <Button variant="soft" size="sm" loading={locating} onClick={locate}>
          <Icon icon={LocateFixed} />
          {intl.formatMessage({ id: 'address.form.locate' })}
        </Button>
      </div>

      <div className={styles.actions}>
        <Button type="submit" variant="primary" loading={create.isPending}>
          {intl.formatMessage({ id: 'address.form.save' })}
        </Button>
        <Button variant="ghost" onClick={onCancel} disabled={create.isPending}>
          {intl.formatMessage({ id: 'address.form.cancel' })}
        </Button>
      </div>
    </form>
  );
}
