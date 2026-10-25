import { Sparkles } from 'lucide-react';
import { useIntl } from 'react-intl';
import { Icon } from '../../shell/Icon';
import styles from '../../ai-drafts/ai.module.css';

/**
 * An AI-R4 sentence (11 §3): the server words the row from its facts and drops any sentence with
 * a number the facts do not hold, so the text is shown as is, marked as worded by AI.
 */
export function InsightText({ text }: { text: string }) {
  const intl = useIntl();
  return (
    <>
      {text}{' '}
      <span className={styles.aiMark}>
        <Icon icon={Sparkles} />
        {intl.formatMessage({ id: 'ai.insight.worded' })}
      </span>
    </>
  );
}
