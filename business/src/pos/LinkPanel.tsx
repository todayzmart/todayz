// "Link customer" at the counter (B-05, D-07): show the bill QR, type a counter code, or take the
// phone number only. Nothing here changes the bill; it only sets the sale's link.
import { newUlid } from '@todayz/auth';
import { Banner, Button, Sheet, TextField } from '@todayz/ui';
import { ApiError } from '@todayz/api-client';
import { Phone, QrCode as QrIcon, UserX, Hash } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useIntl } from 'react-intl';
import type { BillClaim, BillClaimStatus } from './api';
import { billQrUrl, normalizeIndianPhone } from './links';
import { QrCode } from './QrCode';
import { usePos } from './context';
import type { SaleLink } from './types';
import styles from './pos.module.css';

/** How often the screen asks whether the customer has scanned the QR. */
export const CLAIM_POLL_MS = 2000;

type View = 'menu' | 'qr' | 'code' | 'phone';

export interface LinkPanelProps {
  open: boolean;
  onClose: () => void;
  /** Client id of the sale being built; the claim and a verified code are tied to it. */
  saleClientId: string;
  /** What the QR asks the customer to pay (the bill before credits). */
  amountPaise: number;
  /** Credits can be spent on this bill (D-30). */
  creditsAllowed: boolean;
  phone: string;
  onLink: (link: SaleLink | null, amountPaise?: number) => void;
}

export function LinkPanel({ open, onClose, saleClientId, amountPaise, creditsAllowed, phone, onLink }: LinkPanelProps) {
  const intl = useIntl();
  const [view, setView] = useState<View>('menu');
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) setView('menu');
  }
  const back = (
    <Button variant="ghost" size="sm" onClick={() => setView('menu')}>
      {intl.formatMessage({ id: 'pos.link.back' })}
    </Button>
  );
  const done = (link: SaleLink | null, amount?: number) => {
    onLink(link, amount);
    onClose();
  };
  return (
    <Sheet open={open} onClose={onClose} title={intl.formatMessage({ id: 'pos.link.title' })} closeLabel={intl.formatMessage({ id: 'pos.close' })}>
      <div className={styles.sheetBody}>
        {view === 'menu' && <Menu onPick={setView} onSkip={() => done(null)} creditsAllowed={creditsAllowed} />}
        {view === 'qr' && <BillQr saleClientId={saleClientId} amountPaise={amountPaise} creditsAllowed={creditsAllowed} onLink={done} back={back} />}
        {view === 'code' && <CounterCode saleClientId={saleClientId} phone={phone} onLink={done} back={back} />}
        {view === 'phone' && <PhoneOnly phone={phone} onLink={done} back={back} />}
      </div>
    </Sheet>
  );
}

function Menu({ onPick, onSkip, creditsAllowed }: { onPick: (v: View) => void; onSkip: () => void; creditsAllowed: boolean }) {
  const intl = useIntl();
  const { online } = usePos();
  return (
    <>
      {!creditsAllowed && (
        <Banner>{intl.formatMessage({ id: 'pos.link.noCredits' })}</Banner>
      )}
      <ul className={styles.options}>
        <li>
          <Button className={styles.option} block icon={QrIcon} disabled={!online} onClick={() => onPick('qr')}>
            {intl.formatMessage({ id: 'pos.link.qr' })}
          </Button>
          <span className="tz-sub">{intl.formatMessage({ id: online ? 'pos.link.qrHelp' : 'pos.link.qrOffline' })}</span>
        </li>
        <li>
          <Button className={styles.option} block icon={Hash} onClick={() => onPick('code')}>
            {intl.formatMessage({ id: 'pos.link.code' })}
          </Button>
          <span className="tz-sub">{intl.formatMessage({ id: 'pos.link.codeHelp' })}</span>
        </li>
        <li>
          <Button className={styles.option} block icon={Phone} onClick={() => onPick('phone')}>
            {intl.formatMessage({ id: 'pos.link.phone' })}
          </Button>
          <span className="tz-sub">{intl.formatMessage({ id: 'pos.link.phoneHelp' })}</span>
        </li>
        <li>
          <Button className={styles.option} block variant="ghost" icon={UserX} onClick={onSkip}>
            {intl.formatMessage({ id: 'pos.link.skip' })}
          </Button>
        </li>
      </ul>
    </>
  );
}

function BillQr({
  saleClientId,
  amountPaise,
  creditsAllowed,
  onLink,
  back,
}: {
  saleClientId: string;
  amountPaise: number;
  creditsAllowed: boolean;
  onLink: (link: SaleLink | null, amountPaise?: number) => void;
  back: React.ReactNode;
}) {
  const intl = useIntl();
  const { api, now } = usePos();
  const [claim, setClaim] = useState<BillClaim | null>(null);
  const [status, setStatus] = useState<BillClaimStatus | null>(null);
  const [failed, setFailed] = useState(false);
  const [round, setRound] = useState(0);
  const [left, setLeft] = useState<number | null>(null);
  const retryClaim = () => {
    setClaim(null);
    setStatus(null);
    setFailed(false);
    setRound((n) => n + 1);
  };
  // Device clock minus server clock when the claim was made, so the countdown follows the server's expiry.
  const skewRef = useRef(0);

  useEffect(() => {
    let cancelled = false;
    api
      .createBillClaim({ saleClientId, amountPaise }, `claim-${saleClientId}-${amountPaise}-${round}`)
      .then((c) => {
        if (cancelled) return;
        skewRef.current = now() - Date.parse(c.serverNow);
        setClaim(c);
      })
      .catch(() => {
        if (!cancelled) setFailed(true);
      });
    return () => {
      cancelled = true;
    };
  }, [api, saleClientId, amountPaise, round, now]);

  const claimed = status?.status === 'claimed' ? status : null;
  const finish = useCallback(
    (s: BillClaimStatus) => {
      onLink({ method: 'bill_qr', claimId: s.claimId, customerName: s.claimedBy?.displayName ?? null, creditsUsed: creditsAllowed ? (s.creditsRequested ?? 0) : 0 }, amountPaise);
    },
    [onLink, creditsAllowed, amountPaise],
  );

  useEffect(() => {
    if (!claim) return;
    let cancelled = false;
    const id = setInterval(() => {
      api
        .getBillClaim(claim.claimId)
        .then((s) => {
          if (!cancelled) setStatus(s);
        })
        .catch((e: unknown) => {
          // A network blip is retried on the next tick; the claim itself stays valid for 120 s.
          if (e instanceof ApiError && e.status === 404 && !cancelled) setFailed(true);
        });
    }, CLAIM_POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [api, claim]);

  useEffect(() => {
    if (claimed) finish(claimed);
  }, [claimed, finish]);

  useEffect(() => {
    if (!claim) return;
    const tick = () => setLeft(Math.max(0, Math.ceil((Date.parse(claim.expiresAt) - (now() - skewRef.current)) / 1000)));
    tick();
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, [claim, now]);

  const expired = status?.status === 'expired' || status?.status === 'cancelled' || (left !== null && left <= 0);

  return (
    <div className={styles.qrBox}>
      {failed && (
        <Banner tone="warning" live="status">
          {intl.formatMessage({ id: 'pos.link.qrFailed' })}
        </Banner>
      )}
      {claim && !expired && (
        <>
          <QrCode className={styles.qr} text={billQrUrl(claim.token)} label={intl.formatMessage({ id: 'pos.link.qrLabel' })} />
          <p role="status" aria-live="polite">
            {intl.formatMessage({ id: 'pos.link.waiting' })}
            {left !== null && <span className="tz-sub"> {intl.formatMessage({ id: 'pos.link.expiresIn' }, { seconds: left })}</span>}
          </p>
        </>
      )}
      {claim && expired && (
        <>
          <p role="status">{intl.formatMessage({ id: 'pos.link.qrExpired' })}</p>
          <Button variant="primary" onClick={() => retryClaim()}>
            {intl.formatMessage({ id: 'pos.link.newQr' })}
          </Button>
        </>
      )}
      {!claim && !failed && <p role="status">{intl.formatMessage({ id: 'pos.link.qrPreparing' })}</p>}
      {failed && (
        <Button variant="primary" onClick={() => retryClaim()}>
          {intl.formatMessage({ id: 'pos.link.retry' })}
        </Button>
      )}
      {back}
    </div>
  );
}

function CounterCode({
  saleClientId,
  phone: initialPhone,
  onLink,
  back,
}: {
  saleClientId: string;
  phone: string;
  onLink: (link: SaleLink | null) => void;
  back: React.ReactNode;
}) {
  const intl = useIntl();
  const { api, engine, online, now } = usePos();
  const [phone, setPhone] = useState(initialPhone);
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<'phone' | 'code' | 'mismatch' | 'locked' | null>(null);
  const [triesLeft, setTriesLeft] = useState<number | null>(null);
  const ten = normalizeIndianPhone(phone);

  const submit = async () => {
    if (busy) return;
    if (!ten) return setError('phone');
    if (!/^\d{4}$/.test(code)) return setError('code');
    setError(null);
    setBusy(true);
    try {
      const codeAt = new Date(now()).toISOString();
      const clockOffsetMs = await engine.clockOffsetMs();
      if (online) {
        try {
          // The server ties a good code to this sale, so the same code still verifies when the sale syncs.
          const r = await api.verifyCounterCode({ phone: ten, code, saleClientId }, `code-${newUlid()}`);
          if (!r.linked) {
            setError('mismatch');
            return;
          }
        } catch (e) {
          // No answer is not a wrong code: keep it and let the server check it when the sale syncs (D-12).
          if (e instanceof ApiError && e.status >= 400 && e.status < 500) {
            // Too many wrong tries (on this bill, this phone or this cashier) is one locked state.
            const left = e.problem.extensions?.attemptsLeft;
            setTriesLeft(typeof left === 'number' ? left : null);
            setError(e.code === 'code_invalid' ? 'mismatch' : e.code.endsWith('locked') ? 'locked' : 'mismatch');
            return;
          }
        }
      }
      onLink({ method: 'counter_code', phone: ten, code, codeAt, clockOffsetMs });
    } finally {
      setBusy(false);
    }
  };

  return (
    <form
      className={styles.sheetBody}
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <TextField
        label={intl.formatMessage({ id: 'pos.link.phoneLabel' })}
        type="tel"
        inputMode="tel"
        autoComplete="off"
        value={phone}
        onChange={(e) => setPhone(e.target.value)}
        error={error === 'phone' ? intl.formatMessage({ id: 'pos.link.phoneInvalid' }) : undefined}
      />
      <TextField
        label={intl.formatMessage({ id: 'pos.link.codeLabel' })}
        help={intl.formatMessage({ id: 'pos.link.codeHint' })}
        inputMode="numeric"
        autoComplete="off"
        maxLength={4}
        value={code}
        onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))}
        error={
          error === 'code'
            ? intl.formatMessage({ id: 'pos.link.codeInvalid' })
            : error === 'mismatch'
              ? intl.formatMessage({ id: triesLeft === null ? 'pos.link.codeMismatch' : 'pos.link.codeMismatchTries' }, { count: triesLeft ?? 0 })
              : error === 'locked'
                ? intl.formatMessage({ id: 'pos.link.codeLocked' })
                : undefined
        }
      />
      {!online && <Banner>{intl.formatMessage({ id: 'pos.link.codeOffline' })}</Banner>}
      <Button type="submit" variant="primary" loading={busy}>
        {intl.formatMessage({ id: 'pos.link.codeSave' })}
      </Button>
      {back}
    </form>
  );
}

function PhoneOnly({ phone: initialPhone, onLink, back }: { phone: string; onLink: (link: SaleLink | null) => void; back: React.ReactNode }) {
  const intl = useIntl();
  const [phone, setPhone] = useState(initialPhone);
  const [invalid, setInvalid] = useState(false);
  return (
    <form
      className={styles.sheetBody}
      onSubmit={(e) => {
        e.preventDefault();
        const ten = normalizeIndianPhone(phone);
        if (!ten) return setInvalid(true);
        onLink({ method: 'phone_unverified', phone: ten });
      }}
    >
      <TextField
        label={intl.formatMessage({ id: 'pos.link.phoneLabel' })}
        help={intl.formatMessage({ id: 'pos.link.phoneHelp' })}
        type="tel"
        inputMode="tel"
        autoComplete="off"
        value={phone}
        onChange={(e) => setPhone(e.target.value)}
        error={invalid ? intl.formatMessage({ id: 'pos.link.phoneInvalid' }) : undefined}
      />
      <Button type="submit" variant="primary">
        {intl.formatMessage({ id: 'pos.link.phoneSave' })}
      </Button>
      {back}
    </form>
  );
}
