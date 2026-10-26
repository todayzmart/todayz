import type { AiConfirmResult } from '@todayz/api-client/gen/model/aiConfirmResult';
import type { AiDraftStatus } from '@todayz/api-client/gen/model/aiDraftStatus';
import { Button, Segmented, Skeleton, StatusPill } from '@todayz/ui';
import { RotateCcw, Sparkles } from 'lucide-react';
import { useId, type ReactNode } from 'react';
import { useIntl } from 'react-intl';
import { assistErrorKey, type Choice } from './draft';
import css from './assist.module.css';

interface CardProps {
  /** The card's heading, e.g. "AI clean-up". */
  title: string;
  /** One sentence on what the assistant drafts, shown before it is asked. */
  intro: string;
  startLabel: string;
  start: () => void;
  reset: () => void;
  working: boolean;
  error: unknown;
  status: AiDraftStatus | undefined;
  /** What the confirm answered: the card says which lines were applied or refused. */
  result: AiConfirmResult | undefined;
  /** The lines and their confirm, shown once the draft is ready. */
  children?: ReactNode;
}

/**
 * The frame every admin assistant shares (AI-A1…A3): ask, wait, then the drafted lines. Everything
 * it shows is labelled a draft; nothing changes until the person confirms the lines they chose.
 */
export function AssistCard({ title, intro, startLabel, start, reset, working, error, status, result, children }: CardProps) {
  const intl = useIntl();
  const id = useId();
  const t = (key: string, values?: Record<string, string | number>) => intl.formatMessage({ id: key }, values);
  const settled = status === 'confirmed' || status === 'discarded';
  return (
    <section className={`tz-card ${css.card}`} aria-labelledby={`${id}-title`} data-testid="ai-assist">
      <div className={css.head}>
        <h2 id={`${id}-title`} className="tz-card-title">
          {title}
        </h2>
        <StatusPill tone="neutral">{t('aiAssist.badge')}</StatusPill>
      </div>
      {status === undefined && !working && (
        <>
          <p className={css.muted}>{intro}</p>
          <div className={css.actions}>
            <Button variant="soft" icon={Sparkles} onClick={start}>
              {startLabel}
            </Button>
          </div>
        </>
      )}
      {working && (
        <div className={css.stack} role="status">
          <p className={css.muted}>{t('aiAssist.working')}</p>
          <Skeleton shape="block" height="72px" />
        </div>
      )}
      {error !== null && error !== undefined && !working && (
        <p role="alert" className="tz-field-error">
          {t(`aiAssist.error.${assistErrorKey(error)}`)}
        </p>
      )}
      {status === 'failed' && (
        <p role="alert" className="tz-field-error">
          {t('aiAssist.failed')}
        </p>
      )}
      {status === 'ready' && (
        <>
          <p className={css.muted}>{t('aiAssist.check')}</p>
          {children}
        </>
      )}
      {result && result.status === 'ready' && <p className={css.muted}>{t('aiAssist.partly')}</p>}
      {settled && <p role="status">{status === 'confirmed' ? t('aiAssist.applied') : t('aiAssist.discarded')}</p>}
      {(settled || status === 'failed' || (error !== null && error !== undefined && !working && status === undefined)) && (
        <div className={css.actions}>
          <Button variant="ghost" size="sm" icon={RotateCcw} onClick={reset}>
            {t('aiAssist.again')}
          </Button>
        </div>
      )}
    </section>
  );
}

interface LineProps {
  /** The line's heading, e.g. "Merge into". */
  title: string;
  choice: Choice;
  onChoice: (c: Choice) => void;
  /** False when the line takes no edit (a summary): only use and skip. */
  editable?: boolean;
  /** The use option's label when using means sending or applying, e.g. "Send this reply". */
  useLabel?: string;
  /** False when the person may not apply this line (`canApply`): it is shown, but stays skipped. */
  canApply?: boolean;
  /** The confirm's reason for this line (422 field or refusal). */
  problem?: string | undefined;
  /** What the line drafted, read-only. */
  children: ReactNode;
  /** The edit fields, shown while `edited` is chosen. */
  edit?: ReactNode;
}

/** One drafted line: what the assistant wrote and the person's choice to use it, edit it or skip it. */
export function AssistLine({ title, choice, onChoice, editable = true, useLabel, canApply = true, problem, children, edit }: LineProps) {
  const intl = useIntl();
  const t = (key: string) => intl.formatMessage({ id: key });
  const options = [
    { value: 'accepted' as const, label: useLabel ?? t('aiAssist.choice.use') },
    ...(editable ? [{ value: 'edited' as const, label: t('aiAssist.choice.edit') }] : []),
    { value: 'discarded' as const, label: t('aiAssist.choice.skip') },
  ];
  return (
    <div className={css.line}>
      <h3 className={css.lineTitle}>{title}</h3>
      <div className={css.drafted}>{children}</div>
      {canApply ? (
        <Segmented<Choice> label={intl.formatMessage({ id: 'aiAssist.choice.label' }, { line: title })} options={options} value={choice} onChange={onChoice} />
      ) : (
        <p className={css.muted}>{t('aiAssist.notPermitted')}</p>
      )}
      {canApply && choice === 'edited' && edit}
      {problem && (
        <p role="alert" className="tz-field-error">
          {problem}
        </p>
      )}
    </div>
  );
}
