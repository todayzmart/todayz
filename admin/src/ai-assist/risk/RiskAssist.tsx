import { confirmRiskSummaryDraft, createRiskSummaryDraft, getRiskSummaryDraft } from '@todayz/api-client/gen/ai/ai';
import type { RiskFlag } from '@todayz/api-client/gen/model/riskFlag';
import type { RiskSummaryDraft } from '@todayz/api-client/gen/model/riskSummaryDraft';
import { Button, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { Check } from 'lucide-react';
import { useId, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { flagKey } from '../../risk/queries';
import { AssistCard, AssistLine } from '../AssistCard';
import { assistErrorKey, decision, lineReasons, useAssistDraft, type Choice } from '../draft';
import css from '../assist.module.css';

/** The contract bound on RiskSummaryLineValue.text. */
const MAX_TEXT = 600;

/**
 * AI-A3 in the A-13 flag detail: a short summary of what the rule counted. Every number in it is one
 * of the flag's own (D-230); an edit must keep to them too, or the server refuses it as ungrounded.
 * Using it records the summary on the flag's audit trail; it never clears or confirms the flag.
 */
export function RiskAssist({ flag }: { flag: RiskFlag }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const a = useAssistDraft<RiskSummaryDraft, Parameters<typeof confirmRiskSummaryDraft>[1]>({
    key: ['admin', 'ai', 'risk', flag.id],
    create: (headers) => createRiskSummaryDraft({ flagId: flag.id }, { headers }),
    get: (draftId) => getRiskSummaryDraft(draftId),
    confirm: (draftId, body, headers) => confirmRiskSummaryDraft(draftId, body, { headers }),
    settled: () => void qc.invalidateQueries({ queryKey: flagKey(flag.id) }),
  });
  return (
    <AssistCard
      title={intl.formatMessage({ id: 'aiAssist.risk.title' })}
      intro={intl.formatMessage({ id: 'aiAssist.risk.intro' })}
      startLabel={intl.formatMessage({ id: 'aiAssist.risk.start' })}
      start={a.start}
      reset={a.reset}
      working={a.working}
      error={a.error}
      status={a.draft?.status}
      result={a.confirm.data}
    >
      {a.draft && <Line key={a.draft.draftId} d={a.draft} confirm={a.confirm} />}
    </AssistCard>
  );
}

type Confirm = ReturnType<typeof useAssistDraft<RiskSummaryDraft, Parameters<typeof confirmRiskSummaryDraft>[1]>>['confirm'];

function Line({ d, confirm }: { d: RiskSummaryDraft; confirm: Confirm }) {
  const intl = useIntl();
  const fid = useId();
  const toast = useToast();
  const t = (key: string) => intl.formatMessage({ id: key });
  const line = d.lines[0];
  const [choice, setChoice] = useState<Choice>('discarded');
  const [text, setText] = useState(line?.text ?? '');
  if (!line) return <p className={css.muted}>{t('aiAssist.nothing')}</p>;
  const bad = choice === 'edited' && (text.trim() === '' || text.trim().length > MAX_TEXT);
  const reason = lineReasons(confirm.error)[0];

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (bad || confirm.isPending) return;
    confirm.mutate({ lines: [decision(line.lineId, choice, { text: text.trim() })] }, { onSuccess: () => toast.show({ message: t('aiAssist.toast.done') }) });
  };

  return (
    <form className={css.stack} onSubmit={submit} noValidate>
      <AssistLine
        title={t('aiAssist.risk.summary')}
        choice={choice}
        onChoice={setChoice}
        useLabel={t('aiAssist.risk.useSummary')}
        problem={reason ? t(reason === 'ungrounded' ? 'aiAssist.risk.ungrounded' : 'aiAssist.lineInvalid') : undefined}
        edit={
          <div className="tz-field">
            <label className="tz-field-label" htmlFor={`${fid}-text`}>
              {t('aiAssist.risk.summary')}
            </label>
            <textarea id={`${fid}-text`} className="tz-input" rows={4} maxLength={MAX_TEXT} value={text} aria-invalid={bad} aria-describedby={`${fid}-help`} onChange={(e) => setText(e.target.value)} />
            <p id={`${fid}-help`} className="tz-field-help">
              {t('aiAssist.risk.editHelp')}
            </p>
          </div>
        }
      >
        <p>{line.text}</p>
      </AssistLine>
      {bad && (
        <p role="alert" className="tz-field-error">
          {t('aiAssist.lineInvalid')}
        </p>
      )}
      {confirm.error !== null && reason === undefined && (
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
