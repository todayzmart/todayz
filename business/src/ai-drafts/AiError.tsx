import { Banner } from '@todayz/ui';
import { useIntl } from 'react-intl';
import type { AiLineOutcome } from '@todayz/api-client/gen/model/aiLineOutcome';
import { aiErrorKey, refusalId, retryMinutes } from './errors';

/** The banner for a failed AI call: the daily cap says when it lifts; a switched-off feature says so. */
export function AiError({ error }: { error: unknown }) {
  const intl = useIntl();
  if (error === null || error === undefined) return null;
  const key = aiErrorKey(error);
  const mins = key === 'cap' ? retryMinutes(error) : null;
  return (
    <Banner tone="warning">
      <span role="alert">
        {mins !== null ? intl.formatMessage({ id: 'ai.error.capFor' }, { mins }) : intl.formatMessage({ id: `ai.error.${key}` })}
      </span>
    </Banner>
  );
}

/** The line every draft carries: the model only proposes, the person decides. */
export function DraftNote() {
  const intl = useIntl();
  return <p className="tz-field-help">{intl.formatMessage({ id: 'ai.draftNote' })}</p>;
}

/** After a partial confirm: what was added, and that the marked lines can be fixed and confirmed again. */
export function PartialNote({ added }: { added: number }) {
  const intl = useIntl();
  return (
    <Banner tone="warning">
      <span role="alert">{intl.formatMessage({ id: 'ai.partial' }, { n: added })}</span>
    </Banner>
  );
}

/** A settled line's note ("Added" or "Left out"), or a refused line's reason; nothing for an open line. */
export function LineOutcome({ settled, refused, isRefused }: { settled: AiLineOutcome | undefined; refused: string | undefined; isRefused: boolean }) {
  const intl = useIntl();
  if (settled !== undefined) return <span className="tz-field-help">{intl.formatMessage({ id: settled === 'discarded' ? 'ai.line.left' : 'ai.line.added' })}</span>;
  if (!isRefused) return null;
  return (
    <span role="alert" className="tz-field-error">
      {intl.formatMessage({ id: refusalId(refused) })}
    </span>
  );
}
