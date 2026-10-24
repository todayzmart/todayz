// C-12 (C-22, D-110): after the first order of a basket split is placed, "Next: the rest from {shop}" fills
// the cart for the second shop. Shown on an order from the first shop that has not failed.
import { useRouter } from '@tanstack/react-router';
import { Button, Sheet } from '@todayz/ui';
import { ArrowRight } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { fillCart, needsReplace, setPendingSplit, usePendingSplit } from './fill';
import styles from './basket.module.css';

const FAILED = new Set(['rejected', 'expired', 'cancelled', 'delivery_failed']);

export function NextFromSplit({ storeId, status }: { storeId: string; status: string }) {
  const intl = useIntl();
  const router = useRouter();
  const next = usePendingSplit();
  const [replacing, setReplacing] = useState<string | null>(null);

  if (!next || next.afterStoreId !== storeId || FAILED.has(status)) return null;
  const shop = next.store.name;

  const go = () => {
    fillCart(next.store, next.lines);
    setPendingSplit(null);
    router.history.push('/cart');
  };

  return (
    <section className={`tz-card ${styles.next}`} aria-labelledby="basket-next">
      <h2 id="basket-next" className="tz-card-title">
        {intl.formatMessage({ id: 'basket.next.title' }, { shop })}
      </h2>
      <p className="tz-meta">{intl.formatMessage({ id: 'basket.next.body' }, { n: next.lines.length, shop })}</p>
      <div className="tz-chips">
        <Button
          variant="primary"
          icon={ArrowRight}
          onClick={() => {
            const current = needsReplace(next.store);
            if (current) setReplacing(current.name);
            else go();
          }}
        >
          {intl.formatMessage({ id: 'basket.next.fill' }, { shop })}
        </Button>
        <Button variant="ghost" onClick={() => setPendingSplit(null)}>
          {intl.formatMessage({ id: 'basket.next.dismiss' })}
        </Button>
      </div>
      <Sheet
        open={replacing !== null}
        onClose={() => setReplacing(null)}
        title={intl.formatMessage({ id: 'store.replace.title' })}
        description={intl.formatMessage({ id: 'store.replace.body' }, { current: replacing ?? '', next: shop })}
        closeLabel={intl.formatMessage({ id: 'store.replace.close' })}
        footer={
          <>
            <Button variant="soft" onClick={() => setReplacing(null)}>
              {intl.formatMessage({ id: 'store.replace.keep' })}
            </Button>
            <Button variant="primary" onClick={go}>
              {intl.formatMessage({ id: 'store.replace.confirm' })}
            </Button>
          </>
        }
      />
    </section>
  );
}
