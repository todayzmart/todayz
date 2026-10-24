// "Add to basket" on C-05 result cards, C-07 and C-08 (C-22, D-110). Signed out, the line goes into the
// basket on this device. Signed in, a sheet lists the saved baskets (and "New basket"); the pick is read,
// the line added (one more if it is already there) and written back with If-Match, retrying once on a 412.
import type { Basket } from '@todayz/api-client/gen/model/basket';
import { useSessionState } from '@todayz/auth';
import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { Button, Sheet, Skeleton, useToast } from '@todayz/ui';
import { Plus, ShoppingBasket } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { useUnitLabel } from '../home/format';
import { basketKey, basketsKey, isConflict, isLimit, isTooMany, loadBasket, loadBaskets, patchBasket, saveNew, stored } from './api';
import { getDeviceBasket, setDeviceBasket } from './device';
import { addLine, fromBasket, toInput, type EditLine } from './model';
import styles from './basket.module.css';

export interface AddToBasketProps {
  productId: string;
  name: string;
  /** "Fine · 5 kg", shown under the name in the basket. */
  detail?: string;
  /** Smaller button for product cards. */
  compact?: boolean;
}

type Unit = (size: number, unit: string) => string;

async function addToSaved(qc: QueryClient, id: string, line: Omit<EditLine, 'key'>, unit: Unit, retry = true): Promise<Basket> {
  const current = await qc.fetchQuery({ queryKey: basketKey(id), queryFn: () => loadBasket(id), staleTime: 0 });
  try {
    const lines = addLine(fromBasket(current, unit), line);
    const b = await patchBasket(current, { lines: toInput(lines) });
    stored(qc, b);
    return b;
  } catch (err) {
    if (retry && isConflict(err)) return addToSaved(qc, id, line, unit, false);
    throw err;
  }
}

export function AddToBasket({ productId, name, detail = '', compact = false }: AddToBasketProps) {
  const intl = useIntl();
  const toast = useToast();
  const qc = useQueryClient();
  const unitLabel = useUnitLabel();
  const unit: Unit = (size, u) => unitLabel(size, u as Parameters<typeof unitLabel>[1]);
  const { status } = useSessionState();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const line: Omit<EditLine, 'key'> = { productId, text: null, label: name, detail, quantity: 1 };

  const added = (basketName: string, href: string) =>
    toast.show({
      message: intl.formatMessage({ id: 'basket.add.done' }, { name, basket: basketName }),
      action: { label: intl.formatMessage({ id: 'basket.add.view' }), href },
    });

  const failed = (err: unknown) =>
    toast.show({
      message: intl.formatMessage({ id: isLimit(err) ? 'basket.limit' : isTooMany(err) ? 'basket.full' : 'basket.saveError' }),
    });

  const onClick = () => {
    if (status === 'signedIn') {
      setOpen(true);
      return;
    }
    const device = getDeviceBasket();
    setDeviceBasket({ ...device, lines: addLine(device.lines, line) });
    added(device.name || intl.formatMessage({ id: 'basket.device.title' }), '/baskets/device');
  };

  const pick = (id: string, basketName: string) => {
    setBusy(id);
    addToSaved(qc, id, line, unit)
      .then((b) => {
        setOpen(false);
        added(b.name || basketName, `/baskets/${b.basketId}`);
      })
      .catch(failed)
      .finally(() => setBusy(null));
  };

  const createWith = () => {
    setBusy('new');
    saveNew(intl.formatMessage({ id: 'basket.defaultName' }), [{ ...line, key: 'new' }])
      .then((b) => {
        stored(qc, b);
        setOpen(false);
        added(b.name, `/baskets/${b.basketId}`);
      })
      .catch(failed)
      .finally(() => setBusy(null));
  };

  return (
    <>
      <Button
        variant="ghost"
        size="sm"
        icon={ShoppingBasket}
        className={compact ? styles.addCompact : undefined}
        onClick={onClick}
        aria-label={compact ? intl.formatMessage({ id: 'basket.add.labelFor' }, { name }) : undefined}
      >
        {intl.formatMessage({ id: 'basket.add' })}
      </Button>
      {status === 'signedIn' && (
        <Sheet
          open={open}
          onClose={() => setOpen(false)}
          title={intl.formatMessage({ id: 'basket.add.title' })}
          description={name}
          closeLabel={intl.formatMessage({ id: 'basket.add.close' })}
        >
          {open && <BasketPicker busy={busy} onPick={pick} onNew={createWith} />}
        </Sheet>
      )}
    </>
  );
}

function BasketPicker({ busy, onPick, onNew }: { busy: string | null; onPick: (id: string, name: string) => void; onNew: () => void }) {
  const intl = useIntl();
  const list = useQuery({ queryKey: basketsKey, queryFn: loadBaskets });
  return (
    <div className={styles.picker}>
      {list.isPending && <Skeleton shape="block" height="48px" />}
      {list.isError && <p className="tz-meta">{intl.formatMessage({ id: 'basket.error' })}</p>}
      {list.data && list.data.baskets.length > 0 && (
        <ul className={styles.pickList}>
          {list.data.baskets.map((b) => (
            <li key={b.basketId}>
              <Button
                variant="soft"
                block
                loading={busy === b.basketId}
                disabled={busy !== null && busy !== b.basketId}
                onClick={() => onPick(b.basketId, b.name)}
              >
                <span className={styles.pickName}>{b.name}</span>
                <span className="tz-meta">{intl.formatMessage({ id: 'basket.list.count' }, { n: b.lineCount })}</span>
              </Button>
            </li>
          ))}
        </ul>
      )}
      <Button variant="ghost" icon={Plus} block loading={busy === 'new'} disabled={busy !== null && busy !== 'new'} onClick={onNew}>
        {intl.formatMessage({ id: 'basket.add.new' })}
      </Button>
    </div>
  );
}
