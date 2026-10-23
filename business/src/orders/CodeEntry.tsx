import { ApiError } from '@todayz/api-client';
import type { PaymentMethod } from '@todayz/api-client/gen/model/paymentMethod';
import type { StoreOrder } from '@todayz/api-client/gen/model/storeOrder';
import { Banner, Button, OtpInput, Segmented, useToast } from '@todayz/ui';
import { LockKeyhole } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { ActionError } from './ActionError';
import { useOrderAction, verifyCall } from './queries';
import type { CurrentStore } from './store';
import styles from './orders.module.css';

const METHODS = ['cash', 'upi', 'card', 'other'] as const;

/**
 * Hand-over (D-04): the shop types the customer's 4-digit code and says how they paid. The code
 * lives only in this component's state; it is cleared after every answer, never logged, never put
 * in a query key, URL or storage. A wrong code answers `422 code_invalid` with `attemptsLeft`; the
 * 5th wrong code locks entry (`409 code_locked`) until the customer asks for a new code.
 */
export function CodeEntry({ store, order }: { store: CurrentStore; order: StoreOrder }) {
  const intl = useIntl();
  const toast = useToast();
  const action = useOrderAction(store);
  const [code, setCode] = useState('');
  const [method, setMethod] = useState<PaymentMethod | null>(null);
  const [attemptsLeft, setAttemptsLeft] = useState<number | null>(null);
  const [wrongCount, setWrongCount] = useState(0);
  const [locked, setLocked] = useState(false);

  const isLocked = order.codeLocked || locked;
  const left = attemptsLeft ?? order.codeAttemptsLeft;
  const ready = code.length === 4 && method !== null && !isLocked;

  const submit = async () => {
    if (!ready || method === null) return;
    const sent = code;
    const done = await action.run('verify', verifyCall(store, order.id, sent, method));
    setCode('');
    if (done) {
      toast.show({ message: intl.formatMessage({ id: 'orders.code.done' }) });
      return;
    }
  };

  // The failure that needs more than words: a wrong code (attempts left) or a lock.
  const err = action.error;
  let invalidMessage: string | undefined;
  let genericError: unknown = err;
  if (err instanceof ApiError && err.code === 'code_invalid') {
    const n = Number((err.problem.extensions as { attemptsLeft?: unknown } | undefined)?.attemptsLeft);
    invalidMessage = intl.formatMessage({ id: 'orders.code.invalid' }, { left: Number.isFinite(n) ? n : left });
    genericError = null;
    if (Number.isFinite(n) && n !== attemptsLeft) {
      setAttemptsLeft(n);
      setWrongCount((c) => c + 1);
    }
  } else if (err instanceof ApiError && err.code === 'code_locked') {
    genericError = null;
    if (!locked) setLocked(true);
  }

  return (
    <section className={`tz-card ${styles.codeBox}`} aria-labelledby="code-title">
      <h2 id="code-title" className="tz-card-title">
        {intl.formatMessage({ id: 'orders.code.title' })}
      </h2>
      {isLocked ? (
        <Banner tone="warning" icon={LockKeyhole} title={intl.formatMessage({ id: 'orders.code.locked.title' })} live="status">
          {intl.formatMessage({ id: 'orders.code.locked.body' })}
        </Banner>
      ) : (
        <>
          <OtpInput
            label={intl.formatMessage({ id: 'orders.code.label' })}
            digitLabel={(n) => intl.formatMessage({ id: 'orders.code.digit' }, { n })}
            value={code}
            onChange={setCode}
            help={invalidMessage ? undefined : intl.formatMessage({ id: 'orders.code.help' })}
            error={invalidMessage}
            errorKey={wrongCount}
            disabled={action.pending === 'verify'}
          />
          {!invalidMessage && left < 5 && <p className={styles.muted}>{intl.formatMessage({ id: 'orders.code.attemptsLeft' }, { left })}</p>}
          <div className={styles.pay}>
            <span className={styles.payLabel}>{intl.formatMessage({ id: 'orders.code.payment' })}</span>
            <Segmented
              label={intl.formatMessage({ id: 'orders.code.payment' })}
              options={METHODS.map((m) => ({ value: m, label: intl.formatMessage({ id: `orders.code.pay.${m}` }) }))}
              value={method ?? ('' as PaymentMethod)}
              onChange={setMethod}
            />
          </div>
          <ActionError error={genericError} />
          <Button variant="primary" disabled={!ready} loading={action.pending === 'verify'} onClick={() => void submit()}>
            {intl.formatMessage({ id: 'orders.code.submit' })}
          </Button>
        </>
      )}
    </section>
  );
}
