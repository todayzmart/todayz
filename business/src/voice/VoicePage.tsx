import type { VoiceStockAvailability } from '@todayz/api-client/gen/model/voiceStockAvailability';
import type { VoiceStockConfirmResult } from '@todayz/api-client/gen/model/voiceStockConfirmResult';
import type { VoiceStockDraft } from '@todayz/api-client/gen/model/voiceStockDraft';
import type { VoiceStockLanguage } from '@todayz/api-client/gen/model/voiceStockLanguage';
import type { VoiceStockLineResult } from '@todayz/api-client/gen/model/voiceStockLineResult';
import { NATIVE_NAMES, isLocale } from '@todayz/i18n';
import { Banner, Button, ChipGroup, EmptyState, Skeleton, StatusPill } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { useQueryClient } from '@tanstack/react-query';
import { Inbox, Mic, MicOff, Store as StoreIcon } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { useCurrentStore, type CurrentStore } from '../orders/store';
import { useIdempotentAction } from '../stock/action';
import { HoldToTalk } from './HoldToTalk';
import { LineCard } from './LineCard';
import { LineEditor } from './LineEditor';
import { decisions, discardAll, initialChoices, lineState, summarise, type Choice } from './lines';
import { confirmDraft, failureKey, sendRecording, useVoiceAvailability, useVoiceDraft, voiceErrorKey } from './queries';
import type { Recording } from './recorder';
import styles from './voice.module.css';

/** "Detect it": no language is sent and the speech provider works it out (mixed speech too). */
const AUTO = 'und';

/** B-06 Voice update: speak a stock change, check the draft, confirm it. Nothing changes before Confirm. */
export function VoicePage() {
  const intl = useIntl();
  const current = useCurrentStore();
  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'voice.title' })}</h1>
      {current.status === 'loading' && (
        <div role="status" aria-label={intl.formatMessage({ id: 'voice.loading' })}>
          <Skeleton height="6rem" />
        </div>
      )}
      {current.status === 'error' && (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'voice.storeError.title' })}
          description={intl.formatMessage({ id: 'voice.storeError.body' })}
          action={<Button onClick={current.retry}>{intl.formatMessage({ id: 'voice.retry' })}</Button>}
        />
      )}
      {current.status === 'none' && (
        <EmptyState
          icon={StoreIcon}
          title={intl.formatMessage({ id: 'voice.noStore.title' })}
          description={intl.formatMessage({ id: 'voice.noStore.body' })}
          action={
            <Link to="/choose-shop" search={{ next: '/voice' }} className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'voice.noStore.action' })}
            </Link>
          }
        />
      )}
      {current.status === 'ready' && <VoiceDesk store={current.store} />}
    </div>
  );
}

function VoiceDesk({ store }: { store: CurrentStore }) {
  const intl = useIntl();
  const avail = useVoiceAvailability(store);
  if (avail.isPending) {
    return (
      <div role="status" aria-label={intl.formatMessage({ id: 'voice.loading' })}>
        <Skeleton height="10rem" />
      </div>
    );
  }
  if (avail.isError) {
    return (
      <EmptyState
        icon={Inbox}
        title={intl.formatMessage({ id: 'voice.storeError.title' })}
        description={intl.formatMessage({ id: 'voice.storeError.body' })}
        action={<Button onClick={() => void avail.refetch()}>{intl.formatMessage({ id: 'voice.retry' })}</Button>}
      />
    );
  }
  if (avail.data === null) {
    // Off for this shop (a switch, the flag or a provider key): say so, offer nothing to press.
    return (
      <EmptyState
        icon={MicOff}
        title={intl.formatMessage({ id: 'voice.off.title' })}
        description={intl.formatMessage({ id: 'voice.off.body' })}
        action={
          <Link to="/stock" className="tz-btn">
            {intl.formatMessage({ id: 'voice.openStock' })}
          </Link>
        }
      />
    );
  }
  return <VoiceSession store={store} limits={avail.data} />;
}

function VoiceSession({ store, limits }: { store: CurrentStore; limits: VoiceStockAvailability }) {
  const intl = useIntl();
  const [lang, setLang] = useState<string>(AUTO);
  const [draftId, setDraftId] = useState<string | null>(null);
  const [sending, setSending] = useState(false);
  const [sendError, setSendError] = useState<string | null>(null);
  const [result, setResult] = useState<VoiceStockConfirmResult | null>(null);
  const draft = useVoiceDraft(store, draftId);

  const again = () => {
    setDraftId(null);
    setResult(null);
    setSendError(null);
  };

  const onRecorded = async (r: Recording) => {
    setSendError(null);
    if (r.blob.size > limits.maxBytes) {
      setSendError('too_large');
      return;
    }
    setSending(true);
    try {
      setDraftId(await sendRecording(store, r.blob, lang === AUTO ? null : (`${lang}-IN` as VoiceStockLanguage)));
    } catch (err) {
      setSendError(voiceErrorKey(err));
    } finally {
      setSending(false);
    }
  };

  if (result) return <Results draft={draft.data ?? null} status={result.status} outcomes={result.lines} onAgain={again} />;

  if (draftId === null) {
    const spoken = limits.languages.map((l) => l.split('-')[0] ?? '').filter(isLocale);
    return (
      <>
        <p className={styles.intro}>{intl.formatMessage({ id: 'voice.intro' })}</p>
        <div className={styles.field}>
          <span className={styles.label}>
            {intl.formatMessage({ id: 'voice.lang.title' })}
          </span>
          <ChipGroup
            label={intl.formatMessage({ id: 'voice.lang.title' })}
            options={[
              { value: AUTO, label: intl.formatMessage({ id: 'voice.lang.auto' }) },
              ...spoken.map((code) => ({ value: code, label: <span lang={code}>{NATIVE_NAMES[code]}</span> })),
            ]}
            value={lang}
            onChange={setLang}
          />
          <p className={styles.note}>{intl.formatMessage({ id: lang === AUTO ? 'voice.lang.autoHint' : 'voice.lang.fixedHint' })}</p>
        </div>
        <HoldToTalk maxSeconds={limits.maxSeconds} disabled={sending} onRecorded={(r) => void onRecorded(r)} />
        {sending && (
          <p role="status" className={styles.note}>
            {intl.formatMessage({ id: 'voice.sending' })}
          </p>
        )}
        {sendError && (
          <Banner tone="warning" live="status">
            {intl.formatMessage({ id: `voice.error.${sendError}` })}
          </Banner>
        )}
        <p className={styles.note}>{intl.formatMessage({ id: 'voice.privacy' })}</p>
      </>
    );
  }

  if (draft.isError) {
    return (
      <EmptyState
        icon={Inbox}
        title={intl.formatMessage({ id: 'voice.failed.title' })}
        description={intl.formatMessage({ id: `voice.error.${voiceErrorKey(draft.error)}` })}
        action={<Button onClick={again}>{intl.formatMessage({ id: 'voice.again' })}</Button>}
      />
    );
  }

  const d = draft.data;
  if (!d || d.status === 'processing') {
    return (
      <div className={styles.processing} aria-busy="true">
        <p role="status" className={styles.processingTitle}>
          {intl.formatMessage({ id: 'voice.processing.title' })}
        </p>
        <p className={styles.note}>{intl.formatMessage({ id: 'voice.processing.body' })}</p>
        <Skeleton height="4.5rem" shape="block" />
        <Skeleton height="4.5rem" shape="block" />
      </div>
    );
  }
  if (d.status === 'failed') {
    return (
      <EmptyState
        icon={MicOff}
        title={intl.formatMessage({ id: 'voice.failed.title' })}
        description={intl.formatMessage({ id: `voice.failed.${failureKey(d.failure)}` })}
        action={
          <Button variant="primary" icon={Mic} onClick={again}>
            {intl.formatMessage({ id: 'voice.again' })}
          </Button>
        }
      />
    );
  }
  if (d.status === 'confirmed' || d.status === 'discarded') {
    return <Results draft={d} status={d.status} outcomes={d.outcomes} onAgain={again} />;
  }
  return <Review store={store} draft={d} onDone={setResult} onAgain={again} />;
}

function Review({
  store,
  draft,
  onDone,
  onAgain,
}: {
  store: CurrentStore;
  draft: VoiceStockDraft;
  onDone: (r: VoiceStockConfirmResult) => void;
  onAgain: () => void;
}) {
  const intl = useIntl();
  const qc = useQueryClient();
  const action = useIdempotentAction();
  const [choices, setChoices] = useState<Record<string, Choice>>(() => initialChoices(draft.lines));
  const [editing, setEditing] = useState<string | null>(null);
  const sum = summarise(draft.lines, choices, store.role);
  const set = (id: string, c: Choice) => setChoices((old) => ({ ...old, [id]: c }));
  const editLine = editing ? draft.lines.find((l) => l.lineId === editing) : undefined;

  const send = async (lines: ReturnType<typeof discardAll>) => {
    const out = await action.run(JSON.stringify(lines), (key) => confirmDraft(qc, store, draft, lines, key));
    if (out) onDone(out);
  };

  if (draft.lines.length === 0) {
    return (
      <>
        {draft.transcript && <Transcript text={draft.transcript} />}
        <EmptyState
          icon={MicOff}
          title={intl.formatMessage({ id: 'voice.review.none.title' })}
          description={intl.formatMessage({ id: 'voice.review.none.body' })}
          action={
            <Button variant="primary" icon={Mic} onClick={onAgain}>
              {intl.formatMessage({ id: 'voice.again' })}
            </Button>
          }
        />
      </>
    );
  }

  return (
    <>
      {draft.transcript && <Transcript text={draft.transcript} />}
      <p className={styles.note}>{intl.formatMessage({ id: 'voice.review.hint' })}</p>
      {sum.blocked > 0 && (
        <Banner tone="warning" live="none">
          {intl.formatMessage({ id: 'voice.review.blocked' }, { n: sum.blocked })}
        </Banner>
      )}
      {sum.manager > 0 && (
        <Banner tone="info" live="none">
          {intl.formatMessage({ id: 'voice.review.manager' }, { n: sum.manager })}
        </Banner>
      )}
      <ul className={styles.lines} aria-label={intl.formatMessage({ id: 'voice.review.lines' })}>
        {draft.lines.map((l) => {
          const c = choices[l.lineId] ?? { outcome: 'accepted' };
          return (
            <LineCard
              key={l.lineId}
              line={l}
              choice={c}
              state={lineState(l, c, store.role)}
              onEdit={() => setEditing(l.lineId)}
              onDiscard={() => set(l.lineId, { outcome: 'discarded' })}
              onRestore={() => set(l.lineId, { outcome: 'accepted' })}
            />
          );
        })}
      </ul>
      {action.error !== null && (
        <Banner tone="warning" live="status">
          {intl.formatMessage({ id: `voice.error.${voiceErrorKey(action.error)}` })}
        </Banner>
      )}
      <div className={styles.footer}>
        <Button variant="ghost" disabled={action.pending} onClick={() => void send(discardAll(draft.lines))}>
          {intl.formatMessage({ id: 'voice.review.discard' })}
        </Button>
        <Button
          variant="primary"
          loading={action.pending}
          disabled={sum.blocked > 0 || sum.ready + sum.manager === 0}
          onClick={() => void send(decisions(draft.lines, choices, store.role))}
        >
          {intl.formatMessage({ id: 'voice.review.confirm' }, { n: sum.ready })}
        </Button>
      </div>
      {editLine && (
        <LineEditor
          store={store}
          line={editLine}
          choice={choices[editLine.lineId] ?? { outcome: 'accepted' }}
          onClose={() => setEditing(null)}
          onSave={(c) => {
            set(editLine.lineId, c);
            setEditing(null);
          }}
        />
      )}
    </>
  );
}

function Transcript({ text }: { text: string }) {
  const intl = useIntl();
  return (
    <section className={styles.transcript} aria-labelledby="voice-said">
      <h2 id="voice-said" className={styles.label}>
        {intl.formatMessage({ id: 'voice.review.said' })}
      </h2>
      <p>{text}</p>
    </section>
  );
}

const OUTCOME_TONE = { accepted: 'completed', edited: 'completed', discarded: 'cancelled', not_permitted: 'pending' } as const;

function Results({
  draft,
  status,
  outcomes,
  onAgain,
}: {
  draft: VoiceStockDraft | null;
  status: 'confirmed' | 'discarded';
  outcomes: readonly VoiceStockLineResult[];
  onAgain: () => void;
}) {
  const intl = useIntl();
  const applied = outcomes.filter((o) => o.outcome === 'accepted' || o.outcome === 'edited').length;
  const held = outcomes.filter((o) => o.outcome === 'not_permitted').length;
  const name = (id: string) => {
    const l = draft?.lines.find((x) => x.lineId === id);
    return l?.itemName ?? l?.heard ?? intl.formatMessage({ id: 'voice.line.noItem' });
  };
  return (
    <>
      <Banner tone="info" live="status" title={intl.formatMessage({ id: status === 'discarded' ? 'voice.done.discarded' : 'voice.done.title' })}>
        {status === 'confirmed' && intl.formatMessage({ id: 'voice.done.body' }, { applied, held })}
      </Banner>
      {status === 'confirmed' && outcomes.length > 0 && (
        <ul className={styles.results} aria-label={intl.formatMessage({ id: 'voice.done.lines' })}>
          {outcomes.map((o) => (
            <li key={o.lineId} className={styles.resultRow}>
              <span className={styles.grow}>{name(o.lineId)}</span>
              <StatusPill tone={OUTCOME_TONE[o.outcome]}>{intl.formatMessage({ id: `voice.outcome.${o.outcome}` })}</StatusPill>
            </li>
          ))}
        </ul>
      )}
      <div className={styles.footer}>
        <Link to="/stock" className="tz-btn">
          {intl.formatMessage({ id: 'voice.openStock' })}
        </Link>
        <Button variant="primary" icon={Mic} onClick={onAgain}>
          {intl.formatMessage({ id: 'voice.again' })}
        </Button>
      </div>
    </>
  );
}
