import { ApiError } from '@todayz/api-client';
import type { CoachCard } from '@todayz/api-client/gen/model/coachCard';
import { Button, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { Check, CircleAlert, X } from 'lucide-react';
import { useId, useState } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { RouterLink } from '../overview/RouterLink';
import { Icon } from '../shell/Icon';
import { coachActionLabel, coachHref, KIND_LOOK, kindLabel } from './look';
import { actOnCard, type CoachAct } from './queries';
import styles from './coach.module.css';

const day = { day: 'numeric', month: 'short', timeZone: 'Asia/Kolkata' } as const;

/**
 * One coach card in the Demand insights row look (D-99, no metric chip): tinted icon, the type and
 * title, the sentence with its numbers, then one text action that opens a prefilled screen, and Done /
 * Not useful while the card is live. The action sits under the sentence, not beside it, so the text
 * keeps the full width on a phone (D-260). The action changes nothing by itself.
 */
export function CoachCardRow({ store, card, onGone }: { store: CurrentStore; card: CoachCard; onGone?: (id: string) => void }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const titleId = useId();
  const bodyId = useId();
  const [busy, setBusy] = useState<CoachAct | null>(null);
  const look = KIND_LOOK[card.kind];
  const href = coachHref(card.action);
  const live = card.status === 'new' || card.status === 'seen';

  const act = async (what: CoachAct) => {
    setBusy(what);
    try {
      const out = await actOnCard(qc, store, card, what);
      onGone?.(card.id);
      toast.show({
        icon: what === 'done' ? Check : undefined,
        message:
          what === 'done'
            ? intl.formatMessage({ id: 'coach.doneToast' })
            : out.quietUntil
              ? intl.formatMessage({ id: 'coach.hiddenUntil' }, { date: intl.formatDate(new Date(out.quietUntil), day) })
              : intl.formatMessage({ id: 'coach.hidden' }),
      });
    } catch (err) {
      const changed = err instanceof ApiError && err.code === 'invalid_transition';
      toast.show({ icon: CircleAlert, message: intl.formatMessage({ id: changed ? 'coach.error.changed' : 'coach.error.generic' }) });
    } finally {
      setBusy(null);
    }
  };

  const when = card.actedAt ?? card.validUntil;
  return (
    <li className={`tz-insight ${styles.row}`} data-tint={look.tint} data-kind={card.kind} aria-labelledby={titleId}>
      <span className="tz-insight-icon">
        <Icon icon={look.icon} />
      </span>
      <div className={styles.text}>
        <span className={styles.kind}>{kindLabel(intl, card.kind)}</span>
        <b id={titleId}>{card.title}</b>
        <small id={bodyId}>{card.body}</small>
        {card.generatedBy === 'ai' && <span className={styles.ai}>{intl.formatMessage({ id: 'coach.aiWording' })}</span>}
        {live && href && (
          <RouterLink href={href} className={styles.action} aria-describedby={bodyId}>
            {coachActionLabel(intl, card.action)}
            <span aria-hidden="true"> →</span>
          </RouterLink>
        )}
        {live ? (
          <span className={styles.acts}>
            <Button size="sm" variant="soft" icon={Check} loading={busy === 'done'} disabled={busy === 'dismiss'} aria-describedby={titleId} onClick={() => void act('done')}>
              {intl.formatMessage({ id: 'coach.done' })}
            </Button>
            <Button size="sm" variant="ghost" icon={X} loading={busy === 'dismiss'} disabled={busy === 'done'} aria-describedby={titleId} onClick={() => void act('dismiss')}>
              {intl.formatMessage({ id: 'coach.notUseful' })}
            </Button>
          </span>
        ) : (
          <span className={styles.ai}>
            {intl.formatMessage({ id: `coach.status.${card.status}` }, { date: intl.formatDate(new Date(when), day) })}
          </span>
        )}
      </div>
    </li>
  );
}
