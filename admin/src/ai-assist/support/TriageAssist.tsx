import { confirmTicketTriageDraft, createTicketTriageDraft, getTicketTriageDraft } from '@todayz/api-client/gen/ai/ai';
import { AdminAiTicketCategory } from '@todayz/api-client/gen/model/adminAiTicketCategory';
import { AdminAiTicketPriority } from '@todayz/api-client/gen/model/adminAiTicketPriority';
import type { TicketDetail } from '@todayz/api-client/gen/model/ticketDetail';
import type { TicketTriageDraft } from '@todayz/api-client/gen/model/ticketTriageDraft';
import type { TicketTriageLineValue } from '@todayz/api-client/gen/model/ticketTriageLineValue';
import { Button, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { Check } from 'lucide-react';
import { useId, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { useSupportLabels } from '../../support/labels';
import { ticketKey } from '../../support/queries';
import { MAX_BODY } from '../../support/Composer';
import { AssistCard, AssistLine } from '../AssistCard';
import { assistErrorKey, decision, lineReasons, useAssistDraft, type Choice } from '../draft';
import css from '../assist.module.css';

const CATEGORIES = Object.values(AdminAiTicketCategory);
const PRIORITIES = Object.values(AdminAiTicketPriority);

/**
 * AI-A2 on the A-14 ticket (D-234): a category, priority and order guess, a summary once the thread
 * is long, and a reply draft. Using the classify line sets the ticket's category and priority
 * (audited); using the reply sends it to the opener as the agent's own A-14 reply. The summary is
 * read only: it is for the agent and changes nothing.
 */
export function TriageAssist({ detail }: { detail: TicketDetail }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const id = detail.ticket.id;
  const a = useAssistDraft<TicketTriageDraft, Parameters<typeof confirmTicketTriageDraft>[1]>({
    key: ['admin', 'ai', 'triage', id],
    create: (headers) => createTicketTriageDraft({ ticketId: id }, { headers }),
    get: (draftId) => getTicketTriageDraft(draftId),
    confirm: (draftId, body, headers) => confirmTicketTriageDraft(draftId, body, { headers }),
    settled: () => {
      void qc.invalidateQueries({ queryKey: ticketKey(id) });
      void qc.invalidateQueries({ queryKey: ['admin', 'support', 'list'] });
    },
  });
  return (
    <AssistCard
      title={intl.formatMessage({ id: 'aiAssist.triage.title' })}
      intro={intl.formatMessage({ id: 'aiAssist.triage.intro' })}
      startLabel={intl.formatMessage({ id: 'aiAssist.triage.start' })}
      start={a.start}
      reset={a.reset}
      working={a.working}
      error={a.error}
      status={a.draft?.status}
      result={a.confirm.data}
    >
      {a.draft && <Lines key={a.draft.draftId} d={a.draft} confirm={a.confirm} />}
    </AssistCard>
  );
}

type Confirm = ReturnType<typeof useAssistDraft<TicketTriageDraft, Parameters<typeof confirmTicketTriageDraft>[1]>>['confirm'];

function Lines({ d, confirm }: { d: TicketTriageDraft; confirm: Confirm }) {
  const intl = useIntl();
  const fid = useId();
  const toast = useToast();
  const labels = useSupportLabels();
  const t = (key: string, values?: Record<string, string | number>) => intl.formatMessage({ id: key }, values);
  const c = d.classify;
  const done = new Set(d.outcomes.filter((o) => o.outcome !== 'not_permitted').map((o) => o.lineId));
  const [choice, setChoice] = useState<Record<string, Choice>>({});
  const pick = (lineId: string) => choice[lineId] ?? 'discarded';
  const choose = (lineId: string) => (v: Choice) => setChoice((m) => ({ ...m, [lineId]: v }));
  const [category, setCategory] = useState<AdminAiTicketCategory>(c?.category ?? 'other');
  const [priority, setPriority] = useState<AdminAiTicketPriority>(c?.priority ?? 'normal');
  const [reply, setReply] = useState(d.reply?.text ?? '');

  const live = [c, d.summary, d.reply].filter((l): l is NonNullable<typeof l> => l !== null && !done.has(l.lineId));
  const replyBad = d.reply !== null && pick(d.reply.lineId) === 'edited' && (reply.trim() === '' || reply.trim().length > MAX_BODY);
  const reasons = lineReasons(confirm.error);
  const reasonFor = (lineId: string) => (reasons[live.findIndex((l) => l.lineId === lineId)] ? t('aiAssist.lineInvalid') : undefined);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (replyBad || confirm.isPending) return;
    const lines = live.map((l) => {
      let v: TicketTriageLineValue = {};
      if (l === c) v = { category, priority };
      if (l === d.reply) v = { text: reply.trim() };
      // The summary takes no edit: it is used (noted) or skipped.
      return decision(l.lineId, l === d.summary && pick(l.lineId) === 'edited' ? 'accepted' : pick(l.lineId), v);
    });
    confirm.mutate({ lines }, { onSuccess: () => toast.show({ message: t('aiAssist.toast.done') }) });
  };

  if (live.length === 0) return <p className={css.muted}>{t('aiAssist.nothing')}</p>;
  return (
    <form className={css.stack} onSubmit={submit} noValidate>
      {c && !done.has(c.lineId) && (
        <AssistLine
          title={t('aiAssist.triage.classify')}
          choice={pick(c.lineId)}
          onChoice={choose(c.lineId)}
          useLabel={t('aiAssist.triage.useClassify')}
          problem={reasonFor(c.lineId)}
          edit={
            <div className={css.fields}>
              <div className="tz-field">
                <label className="tz-field-label" htmlFor={`${fid}-category`}>
                  {t('aiAssist.triage.category')}
                </label>
                <select id={`${fid}-category`} className="tz-input" value={category} onChange={(e) => setCategory(e.target.value as AdminAiTicketCategory)}>
                  {CATEGORIES.map((v) => (
                    <option key={v} value={v}>
                      {labels.category(v)}
                    </option>
                  ))}
                </select>
              </div>
              <div className="tz-field">
                <label className="tz-field-label" htmlFor={`${fid}-priority`}>
                  {t('aiAssist.triage.priority')}
                </label>
                <select id={`${fid}-priority`} className="tz-input" value={priority} onChange={(e) => setPriority(e.target.value as AdminAiTicketPriority)}>
                  {PRIORITIES.map((v) => (
                    <option key={v} value={v}>
                      {labels.priority(v)}
                    </option>
                  ))}
                </select>
              </div>
            </div>
          }
        >
          <dl className={css.facts}>
            <dt>{t('aiAssist.triage.category')}</dt>
            <dd>{labels.category(c.category)}</dd>
            <dt>{t('aiAssist.triage.priority')}</dt>
            <dd>{labels.priority(c.priority)}</dd>
            <dt>{t('aiAssist.triage.order')}</dt>
            <dd>{c.orderNumber ?? t('aiAssist.none')}</dd>
          </dl>
          <p className={css.muted}>{c.reason}</p>
        </AssistLine>
      )}
      {d.summary && !done.has(d.summary.lineId) && (
        <AssistLine title={t('aiAssist.triage.summary')} choice={pick(d.summary.lineId)} onChoice={choose(d.summary.lineId)} editable={false} useLabel={t('aiAssist.triage.useSummary')} problem={reasonFor(d.summary.lineId)}>
          <p>{d.summary.text}</p>
        </AssistLine>
      )}
      {d.reply && !done.has(d.reply.lineId) && (
        <AssistLine
          title={t('aiAssist.triage.reply')}
          choice={pick(d.reply.lineId)}
          onChoice={choose(d.reply.lineId)}
          useLabel={t('aiAssist.triage.useReply')}
          problem={reasonFor(d.reply.lineId)}
          edit={
            <div className="tz-field">
              <label className="tz-field-label" htmlFor={`${fid}-reply`}>
                {t('aiAssist.triage.replyLabel')}
              </label>
              <textarea id={`${fid}-reply`} className="tz-input" rows={5} maxLength={MAX_BODY} value={reply} aria-invalid={replyBad} onChange={(e) => setReply(e.target.value)} />
            </div>
          }
        >
          <p>{d.reply.text}</p>
        </AssistLine>
      )}
      {replyBad && (
        <p role="alert" className="tz-field-error">
          {t('aiAssist.lineInvalid')}
        </p>
      )}
      {confirm.error !== null && Object.keys(reasons).length === 0 && (
        <p role="alert" className="tz-field-error">
          {t(`aiAssist.error.${assistErrorKey(confirm.error)}`)}
        </p>
      )}
      <div className={css.actions}>
        <Button type="submit" variant="primary" icon={Check} loading={confirm.isPending}>
          {t('aiAssist.confirm')}
        </Button>
      </div>
    </form>
  );
}
