import { ApiError } from '@todayz/api-client';
import { confirmShelfPhotoDraft, createShelfPhotoDraft, getShelfPhotoDraft } from '@todayz/api-client/gen/ai/ai';
import type { AiConfirmResult } from '@todayz/api-client/gen/model/aiConfirmResult';
import type { ShelfPhotoDraft } from '@todayz/api-client/gen/model/shelfPhotoDraft';
import { formatMoney } from '@todayz/i18n';
import { Banner, Button, EmptyState, TextField, ToastProvider } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { Camera } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import { AiError, DraftNote, LineOutcome, PartialNote } from '../../ai-drafts/AiError';
import { lineReasonId, lineReasons } from '../../ai-drafts/errors';
import { outcomesOf } from '../../ai-drafts/partial';
import { useAiFeatures, withKey } from '../../ai-drafts/queries';
import { ScanFlow } from '../../ai-drafts/ScanFlow';
import type { CurrentStore } from '../../orders/store';
import { RouterLink } from '../../overview/RouterLink';
import { StoreGate } from '../../overview/StoreGate';
import { useAction } from '../../stores/queries';
import { addable, decideShelf, startShelf, type ShelfEdit, type ShelfProblem } from './lines';
import styles from '../../ai-drafts/ai.module.css';

/** B-18 Add stock from a shelf photo (AI-R3): photo → catalogue products → the person picks which to list, at what price. Owner and manager. */
export function ShelfScanPage() {
  return (
    <ToastProvider>
      <StoreGate next="/onboarding/shelf">{(store) => <ShelfDesk store={store} />}</StoreGate>
    </ToastProvider>
  );
}

function ShelfDesk({ store }: { store: CurrentStore }) {
  const intl = useIntl();
  const features = useAiFeatures(store);
  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'ai.shelf.title' })}</h1>
      {!features.shelfPhoto ? (
        <EmptyState icon={Camera} title={intl.formatMessage({ id: 'ai.shelf.off' })} action={<RouterLink href="/stock" className="tz-btn">{intl.formatMessage({ id: 'ai.backToStock' })}</RouterLink>} />
      ) : (
        <ScanFlow<ShelfPhotoDraft>
          store={store}
          features={features}
          text={{ desc: 'ai.shelf.desc', pick: 'ai.shelf.pick', reading: 'ai.shelf.reading', failed: 'ai.shelf.failed' }}
          feature="shelf-photo"
          create={(photo, key) => createShelfPhotoDraft(store.bid, store.sid, { photo }, withKey(key))}
          get={(id) => getShelfPhotoDraft(store.bid, store.sid, id)}
          ready={(d, done) => <Products store={store} draft={d} onDone={done} />}
          summary={(r, d, again) => <Summary result={r} draft={d} onAgain={again} />}
        />
      )}
    </div>
  );
}

function Products({ store, draft, onDone }: { store: CurrentStore; draft: ShelfPhotoDraft; onDone: (r: AiConfirmResult) => void }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const action = useAction();
  const [edits, setEdits] = useState<Record<string, ShelfEdit>>(() => Object.fromEntries(draft.lines.map((l) => [l.lineId, startShelf(l)])));
  const [problems, setProblems] = useState<Record<string, ShelfProblem>>({});
  // A partial confirm (status ready) settles some lines; only the open ones are decided again.
  const [partial, setPartial] = useState<AiConfirmResult | null>(null);
  const done = outcomesOf(partial?.lines ?? draft.outcomes);
  const open = draft.lines.filter((l) => !done.settled.has(l.lineId));
  const plan = decideShelf(open, edits);
  const n = plan.decisions.filter((x) => x.outcome !== 'discarded').length;
  const server = lineReasons(action.error);
  const sent = new Map(open.map((l, i) => [l.lineId, i]));

  const edit = (id: string, patch: Partial<ShelfEdit>) => {
    setEdits((all) => ({ ...all, [id]: { ...(all[id] as ShelfEdit), ...patch } }));
    setProblems((p) => {
      const { [id]: _, ...rest } = p;
      return rest;
    });
  };
  const send = async (decisions: typeof plan.decisions, name: string) => {
    const result = await action.run(name, (key) => confirmShelfPhotoDraft(store.bid, store.sid, draft.draftId, { lines: decisions }, withKey(key)));
    if (result) {
      // New store items: every stock list and the Overview read again.
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
      <DraftNote />
      {done.refused.size > 0 && <PartialNote added={done.added} />}
      <ul className={styles.lines} aria-label={intl.formatMessage({ id: 'ai.shelf.lines' })}>
        {draft.lines.map((l) => {
          const settled = done.settled.get(l.lineId);
          const e = settled === undefined ? (edits[l.lineId] ?? startShelf(l)) : { ...startShelf(l), on: settled !== 'discarded' };
          const why = problems[l.lineId];
          const i = sent.get(l.lineId);
          const fromServer = (i === undefined ? undefined : server[i]) ?? {};
          const say = (field: 'pricePaise' | 'openingStock') => {
            const local = field === 'pricePaise' && why === 'price' ? 'price' : field === 'openingStock' && why === 'quantity' ? 'quantity' : undefined;
            const r = local ?? fromServer[field];
            return r ? intl.formatMessage({ id: lineReasonId(r) }) : undefined;
          };
          const lineError = fromServer.productId ?? fromServer.value;
          const rowId = `shelf-${l.lineId}`;
          const p = l.product;
          return (
            <li key={l.lineId} className={styles.line} data-off={!e.on || undefined} aria-labelledby={rowId}>
              <div className={styles.lineHead}>
                <input type="checkbox" id={`${rowId}-on`} checked={e.on} disabled={!addable(l) || settled !== undefined} onChange={(ev) => edit(l.lineId, { on: ev.target.checked })} />
                <label htmlFor={`${rowId}-on`}>
                  <b id={rowId}>{p ? p.name : l.name}</b>
                  <span className={styles.meta}>
                    {p === null
                      ? intl.formatMessage({ id: 'ai.shelf.unmatched' }, { seen: l.name })
                      : l.listed
                        ? intl.formatMessage({ id: 'ai.shelf.listed' })
                        : [p.brand, p.size].filter(Boolean).join(' · ')}
                  </span>
                  {p && p.mrpPaise !== null && <span className={styles.meta}>{intl.formatMessage({ id: 'ai.bill.mrp' }, { mrp: formatMoney(p.mrpPaise) })}</span>}
                </label>
              </div>
              <LineOutcome settled={settled} refused={done.refused.get(l.lineId)} isRefused={done.refused.has(l.lineId)} />
              {addable(l) && e.on && settled === undefined && (
                <div className={styles.fields}>
                  <TextField label={intl.formatMessage({ id: 'ai.shelf.price' })} prefix="₹" inputMode="decimal" value={e.price} error={say('pricePaise')} onChange={(ev) => edit(l.lineId, { price: ev.target.value })} />
                  <TextField
                    label={intl.formatMessage({ id: 'ai.shelf.opening' })}
                    help={intl.formatMessage({ id: 'ai.shelf.openingHelp' })}
                    inputMode="numeric"
                    value={e.opening}
                    error={say('openingStock')}
                    onChange={(ev) => edit(l.lineId, { opening: ev.target.value })}
                  />
                </div>
              )}
              {lineError && (
                <span role="alert" className="tz-field-error">
                  {intl.formatMessage({ id: lineReasonId(lineError) })}
                </span>
              )}
            </li>
          );
        })}
      </ul>
      {conflict ? (
        <Banner tone="warning">
          <span role="alert">{intl.formatMessage({ id: 'ai.error.changed' })}</span>
        </Banner>
      ) : (
        action.error !== null && Object.keys(server).length === 0 && <AiError error={action.error} />
      )}
      <div className={styles.actions}>
        <Button variant="primary" type="submit" loading={action.pending} disabled={n === 0}>
          {intl.formatMessage({ id: 'ai.shelf.confirm' }, { n })}
        </Button>
        <Button variant="ghost" onClick={discard} disabled={action.pending}>
          {intl.formatMessage({ id: done.settled.size > 0 ? 'ai.discardRest' : 'ai.discard' })}
        </Button>
      </div>
    </form>
  );
}

function Summary({ result, draft, onAgain }: { result: AiConfirmResult; draft: ShelfPhotoDraft; onAgain: () => void }) {
  const intl = useIntl();
  const byId = new Map(draft.lines.map((l) => [l.lineId, l]));
  const added = result.lines.filter((r) => r.outcome === 'accepted' || r.outcome === 'edited');
  return (
    <section className={styles.page} aria-labelledby="shelf-done">
      <Banner live="status">
        <span id="shelf-done">{result.status === 'discarded' ? intl.formatMessage({ id: 'ai.shelf.discarded' }) : intl.formatMessage({ id: 'ai.shelf.done' }, { n: added.length })}</span>
      </Banner>
      {added.length > 0 && (
        <ul className={styles.lines}>
          {added.map((r) => {
            const p = byId.get(r.lineId)?.product;
            return p ? <li key={r.lineId}>{p.name}</li> : null;
          })}
        </ul>
      )}
      <div className={styles.actions}>
        <RouterLink href="/stock" className="tz-btn tz-btn-primary">
          {intl.formatMessage({ id: 'ai.backToStock' })}
        </RouterLink>
        <Button onClick={onAgain}>{intl.formatMessage({ id: 'ai.shelf.another' })}</Button>
      </div>
    </section>
  );
}
