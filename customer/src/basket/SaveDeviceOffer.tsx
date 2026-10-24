// BSK-11: once signed in, a basket kept on this device is offered for saving. On yes it is created in the
// account and removed from the device; on "Not now" it stays on the device.
import { useQueryClient } from '@tanstack/react-query';
import { useRouter } from '@tanstack/react-router';
import { Banner, Button } from '@todayz/ui';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { isLimit, isTooMany, saveNew, stored } from './api';
import { clearDeviceBasket, dismissSaveOffer, saveOfferDismissed, useDeviceBasket } from './device';

export function SaveDeviceOffer() {
  const intl = useIntl();
  const qc = useQueryClient();
  const router = useRouter();
  const basket = useDeviceBasket();
  const [hidden, setHidden] = useState(saveOfferDismissed);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<'limit' | 'full' | 'other' | null>(null);

  if (hidden || basket.lines.length === 0) return null;

  const save = () => {
    setBusy(true);
    setError(null);
    saveNew(basket.name || intl.formatMessage({ id: 'basket.defaultName' }), basket.lines)
      .then((b) => {
        stored(qc, b);
        clearDeviceBasket();
        router.history.push(`/baskets/${b.basketId}`);
      })
      .catch((err: unknown) => setError(isLimit(err) ? 'limit' : isTooMany(err) ? 'full' : 'other'))
      .finally(() => setBusy(false));
  };

  return (
    <Banner
      tone="info"
      title={intl.formatMessage({ id: 'basket.saveOffer.title' })}
      action={
        <span className="tz-chips">
          <Button variant="primary" size="sm" loading={busy} onClick={save}>
            {intl.formatMessage({ id: 'basket.saveOffer.save' })}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              dismissSaveOffer();
              setHidden(true);
            }}
          >
            {intl.formatMessage({ id: 'basket.saveOffer.notNow' })}
          </Button>
        </span>
      }
    >
      {error === 'limit'
        ? intl.formatMessage({ id: 'basket.limit' })
        : error === 'full'
          ? intl.formatMessage({ id: 'basket.full' })
          : error === 'other'
            ? intl.formatMessage({ id: 'basket.saveError' })
            : intl.formatMessage({ id: 'basket.saveOffer.body' }, { n: String(basket.lines.length) })}
    </Banner>
  );
}
