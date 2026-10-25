import type { VoiceStockLine } from '@todayz/api-client/gen/model/voiceStockLine';
import { Button, StatusPill } from '@todayz/ui';
import { Pencil, Search, Trash2, Undo2 } from 'lucide-react';
import { useIntl } from 'react-intl';
import { changeText } from './change';
import { effective, LOW_CONFIDENCE, percent, type Choice, type LineState } from './lines';
import styles from './voice.module.css';

export interface LineCardProps {
  line: VoiceStockLine;
  choice: Choice;
  state: LineState;
  onEdit: () => void;
  onDiscard: () => void;
  onRestore: () => void;
}

/** One draft line: the item, what changes, how sure the model was, and what the person can do. */
export function LineCard({ line, choice, state, onEdit, onDiscard, onRestore }: LineCardProps) {
  const intl = useIntl();
  const e = effective(line, choice);
  const unsure = choice.outcome === 'accepted' && line.confidence < LOW_CONFIDENCE;
  const missingNumber = state === 'fix';
  const name = e.itemName ?? intl.formatMessage({ id: 'voice.line.noItem' });

  return (
    <li className={styles.line} data-state={state}>
      <div className={styles.lineHead}>
        <StatusPill tone={state === 'discarded' ? 'cancelled' : state === 'ready' ? 'accepted' : 'pending'}>
          {intl.formatMessage({ id: `voice.kind.${e.kind}` })}
        </StatusPill>
        {state === 'manager' && <span className={styles.flag}>{intl.formatMessage({ id: 'voice.line.askManager' })}</span>}
        {state === 'pick' && <span className={styles.flag}>{intl.formatMessage({ id: 'voice.line.pickFlag' })}</span>}
        {choice.outcome === 'edited' && state !== 'discarded' && <span className={styles.meta}>{intl.formatMessage({ id: 'voice.line.edited' })}</span>}
      </div>
      <p className={styles.lineItem}>{name}</p>
      <p className={styles.lineChange}>
        {missingNumber ? intl.formatMessage({ id: 'voice.line.missingNumber' }) : changeText(intl, e, line.currentPricePaise)}
        {e.kind !== 'set_price' && line.onHand !== null && e.storeItemId === line.storeItemId && (
          <span className={styles.meta}> · {intl.formatMessage({ id: 'voice.line.onHand' }, { n: line.onHand })}</span>
        )}
      </p>
      <p className={styles.heard}>
        {intl.formatMessage({ id: 'voice.line.heard' }, { words: line.heard })}
        {choice.outcome === 'accepted' && (
          <span className={unsure ? styles.unsure : styles.meta}>
            {' · '}
            {intl.formatMessage({ id: unsure ? 'voice.line.unsure' : 'voice.line.sure' }, { pct: percent(line.confidence) })}
          </span>
        )}
      </p>
      {state === 'manager' && <p className={styles.note}>{intl.formatMessage({ id: 'voice.line.managerNote' })}</p>}
      <div className={styles.lineActions}>
        {state === 'discarded' ? (
          <Button size="sm" variant="ghost" icon={Undo2} onClick={onRestore}>
            {intl.formatMessage({ id: 'voice.line.restore' })}
          </Button>
        ) : (
          <>
            {state === 'pick' ? (
              <Button size="sm" variant="primary" icon={Search} onClick={onEdit}>
                {intl.formatMessage({ id: 'voice.line.pick' })}
              </Button>
            ) : (
              <Button size="sm" icon={Pencil} onClick={onEdit}>
                {intl.formatMessage({ id: 'voice.line.edit' })}
              </Button>
            )}
            <Button size="sm" variant="ghost" icon={Trash2} onClick={onDiscard}>
              {intl.formatMessage({ id: 'voice.line.discard' })}
            </Button>
          </>
        )}
      </div>
    </li>
  );
}
