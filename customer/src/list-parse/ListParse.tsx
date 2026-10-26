// C-22 "just ask" with a whole list (AI-C1, D-110, 11 §3): a signed-in customer with list parse on
// pastes or types "1 kg atta, 2 Nandini milk, Dolo 650"; the server turns it into a draft of lines,
// and the customer keeps, edits or removes each one before any of them joins the basket. Reading
// the list saves nothing and searches nothing; the lines go only into the editor, and the customer
// saves and searches the basket themselves (BSK-15). The list text stays in this panel's memory.
import { confirmListParseDraft, createListParseDraft, getListParseDraft } from '@todayz/api-client/gen/ai/ai';
import type { ListParseDecision } from '@todayz/api-client/gen/model/listParseDecision';
import type { ListParseDraft } from '@todayz/api-client/gen/model/listParseDraft';
import type { ListParseLine } from '@todayz/api-client/gen/model/listParseLine';
import { useQuery } from '@tanstack/react-query';
import { Banner, Button, QuantityStepper } from '@todayz/ui';
import { ClipboardList, Trash2, Undo2 } from 'lucide-react';
import { useEffect, useId, useState } from 'react';
import { useIntl } from 'react-intl';
import { clampInt, track } from '../ai/events';
import { isCapReached, useAiFeatures } from '../ai/features';
import { useSend } from '../help/useSend';
import { useStepperLabels } from '../store/itemUi';
import styles from './listParse.module.css';

/** ListParseDraftRequest.text maxLength. */
export const LIST_MAX = 2000;
const TEXT_MAX = 120;
const QTY_MAX = 99;
const MAX_EVENT_LINES = 100;
const POLL_MS = 1000;

/** A line the customer is reviewing: what the draft said, and what they made of it. */
interface Review {
  line: ListParseLine;
  text: string;
  quantity: number;
  removed: boolean;
}

type Phase = { kind: 'closed' } | { kind: 'writing' } | { kind: 'reading'; draftId: string };

export interface ListParseProps {
  /** The kept lines, for the basket editor to add (it merges repeats and caps quantities). */
  onLines: (lines: readonly ListParseLine[]) => void;
}

export function ListParse({ onLines }: ListParseProps) {
  const features = useAiFeatures();
  const [phase, setPhase] = useState<Phase>({ kind: 'closed' });
  if (!features?.listParse) return null;
  return <Panel phase={phase} setPhase={setPhase} onLines={onLines} />;
}

function Panel({ phase, setPhase, onLines }: { phase: Phase; setPhase: (p: Phase) => void; onLines: ListParseProps['onLines'] }) {
  const intl = useIntl();
  const id = useId();
  const [text, setText] = useState('');
  const create = useSend();

  if (phase.kind === 'closed') {
    return (
      <Button variant="soft" size="sm" icon={ClipboardList} onClick={() => setPhase({ kind: 'writing' })}>
        {intl.formatMessage({ id: 'basket.paste.open' })}
      </Button>
    );
  }
  if (phase.kind === 'reading') {
    return (
      <Draft
        draftId={phase.draftId}
        onDone={(lines) => {
          if (lines.length > 0) onLines(lines);
          setText('');
          setPhase({ kind: 'closed' });
        }}
        onRetry={() => setPhase({ kind: 'writing' })}
      />
    );
  }

  const submit = async () => {
    const t = text.trim().slice(0, LIST_MAX);
    if (!t) return;
    const r = await create.run((key) => createListParseDraft({ text: t }, { idempotencyKey: key }));
    if (r) {
      setPhase({ kind: 'reading', draftId: r.draftId });
    }
  };
  if (create.error !== null && isCapReached(create.error)) {
    return <Capped onClose={() => setPhase({ kind: 'closed' })} />;
  }

  return (
    <form
      className={styles.panel}
      aria-label={intl.formatMessage({ id: 'basket.paste.title' })}
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <div className="tz-field">
        <label className="tz-field-label" htmlFor={`${id}-list`}>
          {intl.formatMessage({ id: 'basket.paste.label' })}
        </label>
        <textarea
          id={`${id}-list`}
          className="tz-input"
          rows={4}
          maxLength={LIST_MAX}
          value={text}
          placeholder={intl.formatMessage({ id: 'basket.paste.placeholder' })}
          aria-describedby={`${id}-list-help`}
          onChange={(e) => setText(e.target.value)}
        />
        <p id={`${id}-list-help`} className="tz-field-help">
          {intl.formatMessage({ id: 'basket.paste.help' })}
        </p>
      </div>
      {create.error !== null && (
        <p role="alert" className="tz-field-error">
          {intl.formatMessage({ id: 'basket.paste.sendFailed' })}
        </p>
      )}
      <div className={styles.actions}>
        <Button type="submit" size="sm" loading={create.pending} disabled={!text.trim()}>
          {intl.formatMessage({ id: 'basket.paste.read' })}
        </Button>
        <Button variant="ghost" size="sm" onClick={() => setPhase({ kind: 'closed' })}>
          {intl.formatMessage({ id: 'basket.paste.cancel' })}
        </Button>
      </div>
    </form>
  );
}

function Draft({ draftId, onDone, onRetry }: { draftId: string; onDone: (lines: readonly ListParseLine[]) => void; onRetry: () => void }) {
  const intl = useIntl();
  const draft = useQuery({
    queryKey: ['me', 'ai', 'list-parse', draftId],
    queryFn: () => getListParseDraft(draftId),
    gcTime: 0,
    refetchInterval: (q) => (q.state.data?.status === 'processing' ? POLL_MS : false),
  });
  const d = draft.data;

  if (draft.isError || d?.status === 'failed' || (d?.status === 'ready' && d.lines.length === 0)) {
    return <Failed onRetry={onRetry} onClose={() => onDone([])} />;
  }
  if (!d || d.status === 'processing') {
    return (
      <p className={styles.reading} role="status" aria-busy="true">
        {intl.formatMessage({ id: 'basket.paste.reading' })}
      </p>
    );
  }
  if (d.status !== 'ready') {
    // Already confirmed or discarded (another tab): nothing left to review.
    return <Failed onRetry={onRetry} onClose={() => onDone([])} />;
  }
  return <ReviewLines draft={d} onDone={onDone} />;
}

function Capped({ onClose }: { onClose: () => void }) {
  const intl = useIntl();
  useEffect(() => track('list_parse_used', { outcome: 'cap_reached', lines: 0 }), []);
  return (
    <Banner
      tone="info"
      live="status"
      title={intl.formatMessage({ id: 'basket.paste.capped' })}
      action={
        <Button variant="ghost" size="sm" onClick={onClose}>
          {intl.formatMessage({ id: 'basket.paste.close' })}
        </Button>
      }
    />
  );
}

function Failed({ onRetry, onClose }: { onRetry: () => void; onClose: () => void }) {
  const intl = useIntl();
  useEffect(() => track('list_parse_used', { outcome: 'failed', lines: 0 }), []);
  return (
    <Banner tone="warning" live="status" title={intl.formatMessage({ id: 'basket.paste.failed' })}>
      <div className={styles.actions}>
        <Button variant="soft" size="sm" onClick={onRetry}>
          {intl.formatMessage({ id: 'basket.paste.retry' })}
        </Button>
        <Button variant="ghost" size="sm" onClick={onClose}>
          {intl.formatMessage({ id: 'basket.paste.close' })}
        </Button>
      </div>
    </Banner>
  );
}

function ReviewLines({ draft, onDone }: { draft: ListParseDraft; onDone: (lines: readonly ListParseLine[]) => void }) {
  const intl = useIntl();
  const stepperLabels = useStepperLabels();
  const confirm = useSend();
  const [rows, setRows] = useState<Review[]>(() =>
    draft.lines.map((line) => ({ line, text: line.text, quantity: line.quantity, removed: false })),
  );
  const set = (lineId: string, patch: Partial<Review>) => setRows((rs) => rs.map((r) => (r.line.lineId === lineId ? { ...r, ...patch } : r)));
  const kept = rows.filter((r) => !r.removed);
  const invalid = kept.some((r) => !r.text.trim());

  const decisions = (all: 'review' | 'discard'): ListParseDecision[] =>
    rows.map((r) => {
      if (all === 'discard' || r.removed) return { lineId: r.line.lineId, outcome: 'discarded' };
      const text = r.text.trim().slice(0, TEXT_MAX);
      if (text === r.line.text && r.quantity === r.line.quantity) return { lineId: r.line.lineId, outcome: 'accepted' };
      return { lineId: r.line.lineId, outcome: 'edited', value: { text, quantity: r.quantity } };
    });

  const finish = async (all: 'review' | 'discard') => {
    const res = await confirm.run((key) => confirmListParseDraft(draft.draftId, { lines: decisions(all) }, { idempotencyKey: key }));
    if (all === 'discard') {
      track('list_parse_used', { outcome: 'discarded', lines: 0 });
      onDone([]);
      return;
    }
    if (!res) return;
    const lines = res.basketLines;
    track('list_parse_used', { outcome: lines.length > 0 ? 'confirmed' : 'discarded', lines: clampInt(lines.length, MAX_EVENT_LINES) });
    onDone(lines);
  };

  return (
    <section className={styles.panel} aria-labelledby={`lp-${draft.draftId}`}>
      <h3 id={`lp-${draft.draftId}`} className={styles.title}>
        {intl.formatMessage({ id: 'basket.paste.review.title' })}
      </h3>
      <p className="tz-meta">{intl.formatMessage({ id: 'basket.paste.review.help' })}</p>
      <ul className={styles.rows}>
        {rows.map((r, i) => {
          const name = r.text.trim() || r.line.text;
          return (
            <li key={r.line.lineId} className={styles.row} data-removed={r.removed || undefined}>
              {r.removed ? (
                <>
                  <span className={styles.removedText}>{name}</span>
                  <Button variant="ghost" size="sm" icon={Undo2} onClick={() => set(r.line.lineId, { removed: false })}>
                    {intl.formatMessage({ id: 'basket.paste.review.keep' })}
                  </Button>
                </>
              ) : (
                <>
                  <input
                    className={`tz-input ${styles.rowText}`}
                    aria-label={intl.formatMessage({ id: 'basket.paste.review.item' }, { n: String(i + 1) })}
                    aria-invalid={!r.text.trim() || undefined}
                    value={r.text}
                    maxLength={TEXT_MAX}
                    onChange={(e) => set(r.line.lineId, { text: e.target.value })}
                  />
                  <QuantityStepper
                    quantity={r.quantity}
                    onChange={(q) => set(r.line.lineId, q < 1 ? { removed: true } : { quantity: Math.min(QTY_MAX, q) })}
                    max={QTY_MAX}
                    alwaysStepper
                    size="sm"
                    itemName={name}
                    labels={stepperLabels}
                  />
                  <Button
                    variant="ghost"
                    size="sm"
                    icon={Trash2}
                    aria-label={intl.formatMessage({ id: 'basket.paste.review.remove' }, { name })}
                    onClick={() => set(r.line.lineId, { removed: true })}
                  />
                </>
              )}
            </li>
          );
        })}
      </ul>
      {confirm.error !== null && (
        <p role="alert" className="tz-field-error">
          {intl.formatMessage({ id: 'basket.paste.confirmFailed' })}
        </p>
      )}
      <div className={styles.actions}>
        <Button size="sm" loading={confirm.pending} disabled={kept.length === 0 || invalid} onClick={() => void finish('review')}>
          {intl.formatMessage({ id: 'basket.paste.review.add' }, { count: kept.length })}
        </Button>
        <Button variant="ghost" size="sm" disabled={confirm.pending} onClick={() => void finish('discard')}>
          {intl.formatMessage({ id: 'basket.paste.review.discard' })}
        </Button>
      </div>
    </section>
  );
}
