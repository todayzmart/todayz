import { createFileRoute, useRouter } from '@tanstack/react-router';
import { Button, TextField } from '@todayz/ui';
import { LocateFixed } from 'lucide-react';
import { useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { setPlace } from '../app/place';
import { Icon } from '../shell/Icon';
import styles from '../shell/shell.module.css';

export const Route = createFileRoute('/location')({
  component: LocationScreen,
});

const PIN = /^[1-9][0-9]{5}$/;

/**
 * C-02 location: GPS after an explainer (the browser asks only when the button is tapped), or a
 * 6-digit PIN code. The choice stays on this device.
 */
function LocationScreen() {
  const intl = useIntl();
  const router = useRouter();
  const [locating, setLocating] = useState(false);
  const [gpsError, setGpsError] = useState<'denied' | 'unavailable' | null>(null);
  const [pin, setPin] = useState('');
  const [pinError, setPinError] = useState(false);

  const finish = () => router.history.push('/');

  const useGps = () => {
    if (!('geolocation' in navigator)) {
      setGpsError('unavailable');
      return;
    }
    setGpsError(null);
    setLocating(true);
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        setPlace({ kind: 'gps', lat: pos.coords.latitude, lng: pos.coords.longitude });
        setLocating(false);
        finish();
      },
      (err) => {
        setLocating(false);
        setGpsError(err.code === err.PERMISSION_DENIED ? 'denied' : 'unavailable');
      },
      { enableHighAccuracy: false, timeout: 15_000, maximumAge: 300_000 },
    );
  };

  const savePin = (e: FormEvent) => {
    e.preventDefault();
    if (!PIN.test(pin)) {
      setPinError(true);
      return;
    }
    setPlace({ kind: 'pin', pin });
    finish();
  };

  return (
    <div className={styles.body}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'location.title' })}</h1>
      <p className={styles.heroSub}>{intl.formatMessage({ id: 'location.sub' })}</p>

      <section className={`tz-card ${styles.stack}`} aria-labelledby="loc-gps">
        <span className="tz-iconring">
          <Icon icon={LocateFixed} />
        </span>
        <h2 id="loc-gps" className="tz-card-title">
          {intl.formatMessage({ id: 'location.gps.title' })}
        </h2>
        <p className="tz-card-desc">{intl.formatMessage({ id: 'location.gps.explain' })}</p>
        <Button
          variant="primary"
          block
          loading={locating}
          loadingLabel={intl.formatMessage({ id: 'location.gps.locating' })}
          onClick={useGps}
        >
          {intl.formatMessage({ id: 'location.gps.button' })}
        </Button>
        {gpsError && (
          <p role="alert" className="tz-field-error">
            {intl.formatMessage({ id: gpsError === 'denied' ? 'location.gps.denied' : 'location.gps.unavailable' })}
          </p>
        )}
      </section>

      <form className={`tz-card ${styles.stack}`} onSubmit={savePin} noValidate aria-labelledby="loc-pin">
        <h2 id="loc-pin" className="tz-card-title">
          {intl.formatMessage({ id: 'location.pin.title' })}
        </h2>
        <TextField
          label={intl.formatMessage({ id: 'location.pin.label' })}
          help={intl.formatMessage({ id: 'location.pin.help' })}
          inputMode="numeric"
          autoComplete="postal-code"
          maxLength={6}
          value={pin}
          onChange={(e) => {
            setPin(e.target.value.replace(/\D/g, '').slice(0, 6));
            setPinError(false);
          }}
          error={pinError ? intl.formatMessage({ id: 'location.pin.invalid' }) : undefined}
        />
        <Button type="submit" block>
          {intl.formatMessage({ id: 'location.pin.save' })}
        </Button>
      </form>

      {/* TODO(W1-06): locality search with GET /v1/discover/places/search?q= and /places/reverse?lat=&lng= */}
      <section className={`tz-card ${styles.stack}`} aria-labelledby="loc-search">
        <h2 id="loc-search" className="tz-card-title">
          {intl.formatMessage({ id: 'location.search.title' })}
        </h2>
        <p className="tz-card-desc">{intl.formatMessage({ id: 'location.search.soon' })}</p>
      </section>
    </div>
  );
}
