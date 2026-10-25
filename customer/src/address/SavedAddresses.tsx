// C-18 Account: the saved delivery addresses. Delete, make one the default, or add one (the pin is
// checked against a shop's radius only at checkout, where there is a shop).
import type { Address } from '@todayz/api-client/gen/model/address';
import { Banner, Button, Skeleton } from '@todayz/ui';
import { Plus } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import styles from '../cart/tx.module.css';
import { Icon } from '../shell/Icon';
import { AddressForm } from './AddressForm';
import { addressLine } from './line';
import { useAddressActions, useAddresses } from './queries';

export function SavedAddresses() {
  const intl = useIntl();
  const list = useAddresses(true);
  const { remove, makeDefault } = useAddressActions();
  const [adding, setAdding] = useState(false);
  const [failed, setFailed] = useState(false);
  const busy = remove.isPending || makeDefault.isPending;
  const onError = () => setFailed(true);
  const act = (what: 'remove' | 'makeDefault', a: Address) => {
    setFailed(false);
    if (what === 'remove') remove.mutate(a, { onError });
    else makeDefault.mutate(a, { onError });
  };

  return (
    <section className={`tz-card ${styles.stack}`} aria-labelledby="acct-addresses">
      <h2 id="acct-addresses" className="tz-card-title">
        {intl.formatMessage({ id: 'address.saved.title' })}
      </h2>
      {list.isPending && (
        <div role="status" aria-label={intl.formatMessage({ id: 'address.loading' })}>
          <Skeleton height="3rem" />
        </div>
      )}
      {list.isError && (
        <div role="alert" className={styles.stack}>
          <p>{intl.formatMessage({ id: 'address.saved.loadError' })}</p>
          <Button size="sm" onClick={() => void list.refetch()}>
            {intl.formatMessage({ id: 'address.saved.retry' })}
          </Button>
        </div>
      )}
      {failed && (
        <Banner tone="warning" live="status">
          {intl.formatMessage({ id: 'address.saved.actionError' })}
        </Banner>
      )}
      {list.data && list.data.addresses.length === 0 && !adding && (
        <p className={styles.meta}>{intl.formatMessage({ id: 'address.saved.empty' })}</p>
      )}
      {list.data && list.data.addresses.length > 0 && (
        <ul className={styles.list}>
          {list.data.addresses.map((a) => (
            <li key={a.addressId} className={styles.line}>
              <div className={styles.addressText}>
                <p className={styles.name}>
                  {a.label}
                  {a.isDefault && <> · {intl.formatMessage({ id: 'address.saved.default' })}</>}
                </p>
                <p className={styles.meta}>{addressLine(a)}</p>
              </div>
              <div className={styles.stack}>
                {!a.isDefault && (
                  <Button size="sm" variant="soft" disabled={busy} onClick={() => act('makeDefault', a)}>
                    {intl.formatMessage({ id: 'address.saved.makeDefault' })}
                  </Button>
                )}
                <Button
                  size="sm"
                  variant="soft-danger"
                  disabled={busy}
                  aria-label={intl.formatMessage({ id: 'address.saved.deleteLabel' }, { label: a.label })}
                  onClick={() => act('remove', a)}
                >
                  {intl.formatMessage({ id: 'address.saved.delete' })}
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}
      {adding ? (
        <AddressForm onSaved={() => setAdding(false)} onCancel={() => setAdding(false)} />
      ) : (
        list.data && (
          <Button variant="soft" size="sm" onClick={() => setAdding(true)}>
            <Icon icon={Plus} />
            {intl.formatMessage({ id: 'address.add' })}
          </Button>
        )
      )}
    </section>
  );
}
