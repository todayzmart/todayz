import { confirmBannerCopyDraft, createBannerCopyDraft, getBannerCopyDraft } from '@todayz/api-client/gen/ai/ai';
import type { BannerCopyDraft } from '@todayz/api-client/gen/model/bannerCopyDraft';
import type { BannerCopyDraftRequest } from '@todayz/api-client/gen/model/bannerCopyDraftRequest';
import type { BannerCopyValue } from '@todayz/api-client/gen/model/bannerCopyValue';
import { Button } from '@todayz/ui';
import { Sparkles } from 'lucide-react';
import { useId, useState } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { useAction } from '../stores/queries';
import { AiError } from './AiError';
import { bannerDecisions } from './bannerDecisions';
import { useAiFeatures, useDraft, withKey } from './queries';
import styles from './ai.module.css';

/**
 * AI-R9 in the banner editor: wording options for the picked offer or items. "Use this" fills the
 * editor's fields with the picked option; the banner itself is still saved by the editor's own
 * create, so a suggestion never becomes a banner without the person.
 */
export function BannerCopy({ store, request, onUse }: { store: CurrentStore; request: BannerCopyDraftRequest | null; onUse: (copy: BannerCopyValue) => void }) {
  const intl = useIntl();
  const features = useAiFeatures(store);
  const action = useAction();
  const name = useId();
  const [draftId, setDraftId] = useState<string | null>(null);
  const create = useAction();
  const [pick, setPick] = useState<string | null>(null);
  // Which button sent the call, so only that one shows the spinner.
  const [using, setUsing] = useState(true);
  const draft = useDraft<BannerCopyDraft>(store, 'banner-copy', draftId, (id) => getBannerCopyDraft(store.bid, store.sid, id));
  if (!features.bannerCopy || request === null) return null;

  const d = draft.data;
  const close = () => {
    setDraftId(null);
    setPick(null);
  };
  const start = async () => {
    // Keyed per request: a retry after a dropped connection gets the same draft, not a second call.
    const made = await create.run(`create:${JSON.stringify(request)}`, (key) => createBannerCopyDraft(store.bid, store.sid, request, withKey(key)));
    if (made) setDraftId(made.draftId);
  };
  const decide = async (chosen: string | null) => {
    if (!d) return;
    setUsing(chosen !== null);
    const res = await action.run(chosen ? 'use' : 'discard', (key) => confirmBannerCopyDraft(store.bid, store.sid, d.draftId, { lines: bannerDecisions(d, chosen) }, withKey(key)));
    if (!res) return;
    if (res.copy) onUse(res.copy);
    close();
  };

  if (draftId === null || (d && d.status !== 'processing' && d.status !== 'ready' && d.status !== 'failed')) {
    return (
      <div className={styles.aiBox}>
        <Button size="sm" icon={Sparkles} loading={create.pending} onClick={() => void start()}>
          {intl.formatMessage({ id: 'ai.banner.suggest' })}
        </Button>
        <AiError error={create.error} />
      </div>
    );
  }
  if (draft.isError || d?.status === 'failed') {
    return (
      <div className={styles.aiBox}>
        {draft.isError ? (
          <AiError error={draft.error} />
        ) : (
          <span role="alert" className="tz-field-error">
            {intl.formatMessage({ id: 'ai.banner.failed' })}
          </span>
        )}
        <Button size="sm" onClick={close}>
          {intl.formatMessage({ id: 'ai.again' })}
        </Button>
      </div>
    );
  }
  if (!d || d.status === 'processing') {
    return (
      <div className={styles.aiBox} role="status">
        <span className={styles.meta}>{intl.formatMessage({ id: 'ai.banner.writing' })}</span>
      </div>
    );
  }
  return (
    <fieldset className={styles.aiBox}>
      <legend className="tz-field-label">{intl.formatMessage({ id: 'ai.banner.options' })}</legend>
      <span className="tz-field-help">{intl.formatMessage({ id: 'ai.draftNote' })}</span>
      <div className={styles.options}>
        {d.lines.map((l) => (
          <label key={l.lineId} className={styles.option} data-tint={l.tint}>
            <input type="radio" name={name} value={l.lineId} checked={pick === l.lineId} onChange={() => setPick(l.lineId)} />
            <span>
              <strong>{l.headline}</strong>
              {l.subline && <span> · {l.subline}</span>}
              {d.language !== 'en' && <span className={styles.meta}>{[l.headlineEn, l.sublineEn].filter(Boolean).join(' · ')}</span>}
            </span>
          </label>
        ))}
      </div>
      <AiError error={action.error} />
      <div className={styles.actions}>
        <Button size="sm" variant="primary" disabled={pick === null || (action.pending && !using)} loading={action.pending && using} onClick={() => void decide(pick)}>
          {intl.formatMessage({ id: 'ai.banner.use' })}
        </Button>
        <Button size="sm" variant="ghost" loading={action.pending && !using} disabled={action.pending && using} onClick={() => void decide(null)}>
          {intl.formatMessage({ id: 'ai.banner.none' })}
        </Button>
      </div>
    </fieldset>
  );
}
