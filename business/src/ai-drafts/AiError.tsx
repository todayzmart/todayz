import { Banner } from '@todayz/ui';
import { useIntl } from 'react-intl';
import { aiErrorKey, retryMinutes } from './errors';

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
