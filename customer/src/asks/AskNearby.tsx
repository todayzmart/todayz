// C-25 Ask sheet (D-101, 01 §11.2): "Ask shops nearby" on C-05 (no results) and C-08, "Ask this shop" on
// C-07. Item (the product, or the typed words), quantity, an optional "Need by", the privacy line and Send.
// Signed out, the fields are kept (draft.ts) and the customer signs in first, then lands back on the sheet.
import { createAsk } from '@todayz/api-client/gen/asks/asks';
import type { AskCreate } from '@todayz/api-client/gen/model/askCreate';
import type { AskNeedWhen } from '@todayz/api-client/gen/model/askNeedWhen';
import { useSessionState } from '@todayz/auth';
import { useQueryClient } from '@tanstack/react-query';
import { useRouter, useRouterState } from '@tanstack/react-router';
import { Button, Chip, QuantityStepper, Sheet, TextField } from '@todayz/ui';
import { MessageCircleQuestion, Store } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useIntl } from 'react-intl';
import { useCoords } from '../home/coords';
import { RouterLink } from '../home/RouterLink';
import { newUlid } from '../orders/ulid';
import { askProblemOf, storedAsk, type AskProblem } from './api';
import { clearDraft, readDraft, saveDraft } from './draft';
import styles from './asks.module.css';

export interface AskNearbyProps {
  /** The product name, or the words the customer searched for. */
  query: string;
  /** The catalogue product, when there is one (C-08); without it the ask is the typed words. */
  productId?: string | undefined;
  /** "Ask this shop": only this shop is asked (C-07). */
  shopSlug?: string | undefined;
}

const NEED: readonly AskNeedWhen[] = ['now', 'today', 'tomorrow'];
const MAX_QTY = 99; // the contract's maximum for AskCreate.quantity

export function AskNearby({ query, productId, shopSlug }: AskNearbyProps) {
  const intl = useIntl();
  const router = useRouter();
  const path = useRouterState({ select: (s) => s.location.href });
  const slot = productId ? `p:${productId}` : shopSlug ? `s:${shopSlug}` : `q:${query}`;
  // Back from sign-in with a draft for this slot: the sheet opens with what the customer had typed.
  const [draft] = useState(() => readDraft(path, slot));
  const [open, setOpen] = useState(draft !== null);
  const [text, setText] = useState(draft?.text ?? query);
  const [quantity, setQuantity] = useState(draft?.quantity ?? 1);
  const [needBy, setNeedBy] = useState<AskNeedWhen | null>(draft?.needBy ?? null);
  useEffect(() => {
    if (draft) clearDraft();
  }, [draft]);

  const label = intl.formatMessage({ id: shopSlug ? 'asks.cta.shop' : 'asks.cta.nearby' });
  return (
    <>
      <Button
        variant="soft"
        icon={shopSlug ? Store : MessageCircleQuestion}
        className={styles.cta}
        onClick={() => {
          setText((t) => t || query);
          setOpen(true);
        }}
      >
        {label}
      </Button>
      <Sheet
        open={open}
        onClose={() => setOpen(false)}
        title={label}
        description={intl.formatMessage({ id: shopSlug ? 'asks.sheet.shop' : 'asks.sheet.nearby' })}
        closeLabel={intl.formatMessage({ id: 'asks.sheet.close' })}
      >
        {open && (
          <AskForm
            fixedName={productId ? query : null}
            text={text}
            onText={setText}
            quantity={quantity}
            onQuantity={setQuantity}
            needBy={needBy}
            onNeedBy={setNeedBy}
            onSignIn={() => {
              saveDraft({ path, slot, text, quantity, needBy });
              router.history.push(`/sign-in?next=${encodeURIComponent(path)}`);
            }}
            build={(coords) => ({
              ...(productId ? { productId } : { query: text.trim() }),
              quantity,
              ...(needBy ? { needBy } : {}),
              lat: coords.lat,
              lng: coords.lng,
              ...(shopSlug ? { storeSlug: shopSlug } : {}),
            })}
          />
        )}
      </Sheet>
    </>
  );
}

interface AskFormProps {
  fixedName: string | null;
  text: string;
  onText: (t: string) => void;
  quantity: number;
  onQuantity: (n: number) => void;
  needBy: AskNeedWhen | null;
  onNeedBy: (n: AskNeedWhen | null) => void;
  onSignIn: () => void;
  build: (coords: { lat: number; lng: number }) => AskCreate;
}

function AskForm({ fixedName, text, onText, quantity, onQuantity, needBy, onNeedBy, onSignIn, build }: AskFormProps) {
  const intl = useIntl();
  const qc = useQueryClient();
  const router = useRouter();
  const { status } = useSessionState();
  const coords = useCoords();
  const [sending, setSending] = useState(false);
  const [problem, setProblem] = useState<AskProblem | null>(null);
  // One key per attempt: a retry of the same ask is the same request (04 §1.6).
  const attempt = useRef<{ sig: string; key: string } | null>(null);

  const name = fixedName ?? text.trim();
  const send = async () => {
    if (status !== 'signedIn') {
      onSignIn();
      return;
    }
    if (coords.status !== 'ready' || !name) return;
    const body = build(coords.coords);
    const sig = JSON.stringify(body);
    if (attempt.current?.sig !== sig) attempt.current = { sig, key: newUlid() };
    setSending(true);
    setProblem(null);
    try {
      const d = await createAsk(body, { idempotencyKey: attempt.current.key });
      attempt.current = null;
      storedAsk(qc, d);
      router.history.push(`/asks/${d.ask.askId}`);
    } catch (err) {
      setProblem(askProblemOf(err));
    } finally {
      setSending(false);
    }
  };

  return (
    <div className={styles.form}>
      {fixedName !== null ? (
        <p className={styles.item}>{fixedName}</p>
      ) : (
        <TextField
          label={intl.formatMessage({ id: 'asks.sheet.item' })}
          value={text}
          maxLength={200}
          onChange={(e) => {
            onText(e.target.value);
            setProblem(null);
          }}
        />
      )}
      <div className={styles.row}>
        <span className={styles.rowLabel}>{intl.formatMessage({ id: 'asks.sheet.quantity' })}</span>
        <QuantityStepper
          quantity={quantity}
          onChange={(n) => onQuantity(Math.max(1, Math.min(MAX_QTY, n)))}
          max={MAX_QTY}
          alwaysStepper
          itemName={name}
          labels={{
            add: intl.formatMessage({ id: 'asks.sheet.add' }),
            decrement: intl.formatMessage({ id: 'asks.sheet.less' }),
            increment: intl.formatMessage({ id: 'asks.sheet.more' }),
          }}
        />
      </div>
      <fieldset className={styles.need}>
        <legend className={styles.rowLabel}>{intl.formatMessage({ id: 'asks.sheet.needBy' })}</legend>
        <div className={styles.chips}>
          {NEED.map((n) => (
            <Chip key={n} pressed={needBy === n} onPressedChange={(p) => onNeedBy(p ? n : null)}>
              {intl.formatMessage({ id: `asks.need.${n}` })}
            </Chip>
          ))}
        </div>
      </fieldset>
      <p className="tz-meta">{intl.formatMessage({ id: 'asks.sheet.privacy' })}</p>
      {coords.status === 'none' && (
        <p className={styles.note}>
          {intl.formatMessage({ id: 'asks.sheet.noPlace' })}{' '}
          <RouterLink href="/location">{intl.formatMessage({ id: 'asks.sheet.setPlace' })}</RouterLink>
        </p>
      )}
      {coords.status === 'error' && (
        <p className={styles.note}>
          {intl.formatMessage({ id: 'asks.sheet.placeError' })}{' '}
          <Button variant="ghost" size="sm" onClick={coords.retry}>
            {intl.formatMessage({ id: 'asks.retry' })}
          </Button>
        </p>
      )}
      {problem && <ProblemLine problem={problem} />}
      <Button
        variant="brand"
        block
        loading={sending || (status === 'signedIn' && coords.status === 'loading')}
        disabled={!name || (status === 'signedIn' && (coords.status === 'none' || coords.status === 'error'))}
        onClick={() => void send()}
      >
        {intl.formatMessage({ id: status === 'signedIn' ? 'asks.sheet.send' : 'asks.sheet.signIn' })}
      </Button>
    </div>
  );
}

function ProblemLine({ problem }: { problem: AskProblem }) {
  const intl = useIntl();
  switch (problem.kind) {
    case 'limit':
      return (
        <p className={styles.problem} role="alert">
          {problem.limit !== null
            ? intl.formatMessage({ id: 'asks.error.limit' }, { n: problem.limit })
            : intl.formatMessage({ id: 'asks.error.limitPlain' })}
        </p>
      );
    case 'open':
      return (
        <p className={styles.problem} role="alert">
          {intl.formatMessage({ id: 'asks.error.open' })}{' '}
          {problem.askId && <RouterLink href={`/asks/${problem.askId}`}>{intl.formatMessage({ id: 'asks.error.openLink' })}</RouterLink>}
        </p>
      );
    case 'no_shops':
      return <p className={styles.problem} role="alert">{intl.formatMessage({ id: 'asks.error.noShops' })}</p>;
    case 'store_unavailable':
      return <p className={styles.problem} role="alert">{intl.formatMessage({ id: 'asks.error.storeUnavailable' })}</p>;
    case 'invalid':
      return <p className={styles.problem} role="alert">{intl.formatMessage({ id: 'asks.error.invalid' })}</p>;
    case 'failed':
      return <p className={styles.problem} role="alert">{intl.formatMessage({ id: 'asks.error.failed' })}</p>;
  }
}
