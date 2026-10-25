// C-11 delivery address: pick one of the saved addresses (those outside the shop's radius are shown
// but cannot be picked) or add one with a pin.
import type { Address } from '@todayz/api-client/gen/model/address';
import type { DiscoverStorePage } from '@todayz/api-client/gen/model/discoverStorePage';
import { formatDistance } from '@todayz/i18n';
import { Button, Skeleton } from '@todayz/ui';
import { Plus } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { inRadius } from '../cart/fulfilment';
import styles from '../cart/tx.module.css';
import { Icon } from '../shell/Icon';
import { AddressForm } from './AddressForm';
import { addressLine } from './line';

export function AddressPicker({
  store,
  addresses,
  loading,
  chosen,
  onChoose,
}: {
  store: DiscoverStorePage;
  addresses: readonly Address[];
  loading: boolean;
  chosen: string | null;
  onChoose: (addressId: string) => void;
}) {
  const intl = useIntl();
  const [adding, setAdding] = useState(false);
  if (loading) {
    return (
      <div role="status" aria-label={intl.formatMessage({ id: 'address.loading' })}>
        <Skeleton height="3rem" />
      </div>
    );
  }
  const outside = intl.formatMessage({ id: 'address.outside' }, { distance: formatDistance(store.deliveryRadiusM) });
  return (
    <div className={styles.stack}>
      {addresses.length > 0 && (
        <fieldset className={styles.reasons}>
          <legend className={styles.sectionTitle}>{intl.formatMessage({ id: 'address.pick' })}</legend>
          {addresses.map((a) => {
            const fits = inRadius(store, a);
            return (
              <label key={a.addressId} className={styles.addressRow}>
                <input type="radio" name="delivery-address" checked={chosen === a.addressId} disabled={!fits} onChange={() => onChoose(a.addressId)} />
                <span className={styles.addressText}>
                  <span className={styles.name}>{a.label}</span>
                  <span className={styles.meta}>{addressLine(a)}</span>
                  {!fits && <span className={styles.flag}>{outside}</span>}
                </span>
              </label>
            );
          })}
        </fieldset>
      )}
      {addresses.length === 0 && !adding && <p className={styles.meta}>{intl.formatMessage({ id: 'address.none' })}</p>}
      {adding ? (
        <AddressForm
          store={store}
          onCancel={() => setAdding(false)}
          onSaved={(a) => {
            setAdding(false);
            if (inRadius(store, a)) onChoose(a.addressId);
          }}
        />
      ) : (
        <Button variant="soft" size="sm" onClick={() => setAdding(true)}>
          <Icon icon={Plus} />
          {intl.formatMessage({ id: 'address.add' })}
        </Button>
      )}
    </div>
  );
}
