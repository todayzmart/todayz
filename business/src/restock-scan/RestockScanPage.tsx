import { ApiError } from '@todayz/api-client';
import { confirmSupplierBillDraft, createSupplierBillDraft, getSupplierBillDraft } from '@todayz/api-client/gen/ai/ai';
import type { AiConfirmResult } from '@todayz/api-client/gen/model/aiConfirmResult';
import type { SupplierBillDraft } from '@todayz/api-client/gen/model/supplierBillDraft';
import type { SupplierBillLine } from '@todayz/api-client/gen/model/supplierBillLine';
import { formatMoney } from '@todayz/i18n';
import { Banner, Button, EmptyState, TextField, ToastProvider } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { ScanLine } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { AiError, DraftNote, LineOutcome, PartialNote } from '../ai-drafts/AiError';
import { lineReasonId, lineReasons } from '../ai-drafts/errors';
import { outcomesOf } from '../ai-drafts/partial';
import { ScanFlow } from '../ai-drafts/ScanFlow';
import { useAiFeatures, withKey } from '../ai-drafts/queries';
import type { CurrentStore } from '../orders/store';
import { RouterLink } from '../overview/RouterLink';
import { StoreGate } from '../overview/StoreGate';
import { useAction } from '../stores/queries';
import { addCount, decide, startEdit, type LineEdit, type LineProblem } from './lines';
import styles from '../ai-drafts/ai.module.css';

/** B-17 Scan supplier bill (AI-R2): photo → the lines matched to the shop's items → the person confirms the restock. Owner and manager. */
export function RestockScanPage() {
  return (
    <ToastProvider>
      <StoreGate next="/restock-scan">{(store) => <ScanDesk store={store} />}</StoreGate>
    </ToastProvider>
  );
}

function ScanDesk({ store }: { store: CurrentStore }) {
  const intl = useIntl();
  const features = useAiFeatures(store);
  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'ai.bill.title' })}</h1>
      {!features.supplierBill ? (
        <EmptyState icon={ScanLine} title={intl.formatMessage({ id: 'ai.bill.off' })} action={<RouterLink href="/stock" className="tz-btn">{intl.formatMessage({ id: 'ai.backToStock' })}</RouterLink>} />
      ) : (
        <ScanFlow<SupplierBillDraft>
          store={store}
          features={features}
          text={{ desc: 'ai.bill.desc', pick: 'ai.bill.pick', reading: 'ai.bill.reading', failed: 'ai.bill.failed' }}
          feature="supplier-bill"
          create={(photo, key) => createSupplierBillDraft(store.bid, store.sid, { photo }, withKey(key))}
          get={(id) => getSupplierBillDraft(store.bid, store.sid, id)}
          ready={(d, done) => <Lines store={store} draft={d} onDone={done} />}
          summary={(r, d, again) => <Summary result={r} lines={d.lines} onAgain={again} />}
        />
      )}
    </div>
  );
}

function Lines({ store, draft, onDone }: { store: CurrentStore; draft: SupplierBillDraft; onDone: (r: AiConfirmResult) => void }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const action = useAction();
  const [edits, setEdits] = useState<Record<string, LineEdit>>(() => Object.fromEntries(draft.lines.map((l) => [l.lineId, startEdit(l)])));
  const [problems, setProblems] = useState<Record<string, LineProblem>>({});
  // A partial confirm (status ready) settles some lines; only the open ones are decided again.
  const [partial, setPartial] = useState<AiConfirmResult | null>(null);
  const done = outcomesOf(partial?.lines ?? draft.outcomes);
  const open = draft.lines.filter((l) => !done.settled.has(l.lineId));
  const plan = decide(open, edits);
  const n = addCount(plan.decisions);
  const server = lineReasons(action.error);
  const sent = new Map(open.map((l, i) => [l.lineId, i]));

  const edit = (id: string, patch: Partial<LineEdit>) => {
    setEdits((all) => ({ ...all, [id]: { ...(all[id] as LineEdit), ...patch } }));
    setProblems((p) => {
      const { [id]: _, ...rest } = p;
      return rest;
    });
  };
  const send = async (decisions: typeof plan.decisions, name: string) => {
    const result = await action.run(name, (key) => confirmSupplierBillDraft(store.bid, store.sid, draft.draftId, { lines: decisions }, withKey(key)));
    if (result) {
      // The restock changed stock counts: every stock list and the Overview read again.
      void qc.invalidateQueries({ queryKey: ['biz', store.bid, 'store', store.sid] });
      void qc.invalidateQueries({ queryKey: ['stock'] });
      if (result.status === 'ready') setPartial(result);
      else onDone(result);
    }
  };
  const confirm = () => {
    setProblems(plan.problems);
    if (Object.keys(plan.problems).length > 0) return;
    void send(plan.decisions, 'confirm');
  };
  const discard = () => void send(open.map((l) => ({ lineId: l.lineId, outcome: 'discarded' as const })), 'discard');
  const conflict = action.error instanceof ApiError && action.error.code === 'invalid_transition';

  return (
    <form
      className={styles.page}
      noValidate
      onSubmit={(e) => {
        e.preventDefault();
        confirm();
      }}
    >
      {draft.supplier && <p className={styles.sub}>{intl.formatMessage({ id: 'ai.bill.from' }, { supplier: draft.supplier })}</p>}
      <DraftNote />
      {done.refused.size > 0 && <PartialNote added={done.added} />}
      <ul className={styles.lines} aria-label={intl.formatMessage({ id: 'ai.bill.lines' })}>
        {draft.lines.map((l) => {
          const settled = done.settled.get(l.lineId);
          const e = settled === undefined ? (edits[l.lineId] ?? startEdit(l)) : { ...startEdit(l), on: settled !== 'discarded' };
          const why = problems[l.lineId];
          const i = sent.get(l.lineId);
          const fromServer = (i === undefined ? undefined : server[i]) ?? {};
          const say = (field: string) => {
            const local = field === 'quantity' && why === 'quantity' ? 'quantity' : field === 'costPaise' && why === 'cost' ? 'price' : undefined;
            const r = local ?? fromServer[field];
            return r ? intl.formatMessage({ id: lineReasonId(r) }) : undefined;
          };
          const rowId = `bill-${l.lineId}`;
          return (
            <li key={l.lineId} className={styles.line} data-off={!e.on || undefined} aria-labelledby={rowId}>
              <div className={styles.lineHead}>
                <input type="checkbox" id={`${rowId}-on`} checked={e.on} disabled={l.match === null || settled !== undefined} onChange={(ev) => edit(l.lineId, { on: ev.target.checked })} />
                <label htmlFor={`${rowId}-on`}>
                  <b id={rowId}>{l.match ? l.match.itemName : l.name}</b>
                  <span className={styles.meta}>
                    {l.match
                      ? intl.formatMessage({ id: 'ai.bill.matched' }, { bill: l.name, n: l.match.onHand })
                      : intl.formatMessage({ id: 'ai.bill.unmatched' }, { bill: l.name })}
                  </span>
                  {l.mrpPaise !== null && <span className={styles.meta}>{intl.formatMessage({ id: 'ai.bill.mrp' }, { mrp: formatMoney(l.mrpPaise) })}</span>}
                </label>
              </div>
              <LineOutcome settled={settled} refused={done.refused.get(l.lineId)} isRefused={done.refused.has(l.lineId)} />
              {l.match !== null && e.on && settled === undefined && (
                <div className={styles.fields}>
                  <TextField label={intl.formatMessage({ id: 'ai.bill.qty' })} inputMode="numeric" value={e.qty} error={say('quantity')} onChange={(ev) => edit(l.lineId, { qty: ev.target.value })} />
                  <TextField label={intl.formatMessage({ id: 'ai.bill.cost' })} prefix="₹" inputMode="decimal" value={e.cost} error={say('costPaise')} onChange={(ev) => edit(l.lineId, { cost: ev.target.value })} />
                  <TextField label={intl.formatMessage({ id: 'ai.bill.expiry' })} type="date" value={e.expiry} error={say('expiresOn')} onChange={(ev) => edit(l.lineId, { expiry: ev.target.value })} />
                </div>
              )}
              {(fromServer.value ?? (l.match === null ? fromServer.storeItemId : undefined)) && (
                <span role="alert" className="tz-field-error">
                  {intl.formatMessage({ id: lineReasonId(fromServer.value ?? fromServer.storeItemId ?? 'invalid') })}
                </span>
              )}
            </li>
          );
        })}
      </ul>
      {/* TODO(lead): an unmatched line is offered as "create product" in 11 §2; until the add-product sheet takes a prefill, the person adds it in Stock first. */}
      {draft.lines.some((l) => l.match === null) && <p className="tz-field-help">{intl.formatMessage({ id: 'ai.bill.unmatchedHelp' })}</p>}
      {conflict ? <Banner tone="warning"><span role="alert">{intl.formatMessage({ id: 'ai.error.changed' })}</span></Banner> : action.error !== null && Object.keys(server).length === 0 && <AiError error={action.error} />}
      <div className={styles.actions}>
        <Button variant="primary" type="submit" loading={action.pending} disabled={n === 0}>
          {intl.formatMessage({ id: 'ai.bill.confirm' }, { n })}
        </Button>
        <Button variant="ghost" onClick={discard} disabled={action.pending}>
          {intl.formatMessage({ id: done.settled.size > 0 ? 'ai.discardRest' : 'ai.discard' })}
        </Button>
      </div>
    </form>
  );
}

function Summary({ result, lines, onAgain }: { result: AiConfirmResult; lines: SupplierBillLine[]; onAgain: () => void }) {
  const intl = useIntl();
  const added = result.lines.filter((r) => r.outcome === 'accepted' || r.outcome === 'edited');
  const byId = new Map(lines.map((l) => [l.lineId, l]));
  return (
    <section className={styles.page} aria-labelledby="bill-done">
      <Banner live="status">
        <span id="bill-done">
          {result.status === 'discarded' ? intl.formatMessage({ id: 'ai.bill.discarded' }) : intl.formatMessage({ id: 'ai.bill.done' }, { n: added.length })}
        </span>
      </Banner>
      {added.length > 0 && (
        <ul className={styles.lines}>
          {added.map((r) => {
            const l = byId.get(r.lineId);
            return l?.match ? <li key={r.lineId}>{l.match.itemName}</li> : null;
          })}
        </ul>
      )}
      <div className={styles.actions}>
        <RouterLink href="/stock" className="tz-btn tz-btn-primary">{intl.formatMessage({ id: 'ai.backToStock' })}</RouterLink>
        <Button onClick={onAgain}>{intl.formatMessage({ id: 'ai.bill.another' })}</Button>
      </div>
    </section>
  );
}
