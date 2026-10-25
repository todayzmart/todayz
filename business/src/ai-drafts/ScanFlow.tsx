import type { AiConfirmResult } from '@todayz/api-client/gen/model/aiConfirmResult';
import type { AiDraftStatus } from '@todayz/api-client/gen/model/aiDraftStatus';
import type { AiRetailFeatures } from '@todayz/api-client/gen/model/aiRetailFeatures';
import { Banner, Button, Skeleton } from '@todayz/ui';
import { useState, type ReactNode } from 'react';
import { useIntl } from 'react-intl';
import type { CurrentStore } from '../orders/store';
import { useAction } from '../stores/queries';
import { AiError } from './AiError';
import { PhotoPick } from './PhotoPick';
import { useDraft } from './queries';
import styles from './ai.module.css';

interface Draft {
  draftId: string;
  status: AiDraftStatus;
}

export interface ScanFlowProps<D extends Draft> {
  store: CurrentStore;
  features: AiRetailFeatures;
  /** Message ids, spelled out so the message lint sees them. */
  text: { desc: string; pick: string; reading: string; failed: string };
  /** Query key part for the draft. */
  feature: string;
  /** Sends the photo with its Idempotency-Key (the same key for a retry of the same photo). */
  create: (photo: File, key: string) => Promise<{ draftId: string }>;
  get: (id: string) => Promise<D>;
  /** The ready draft; `done` is called with the confirm's answer. */
  ready: (draft: D, done: (r: AiConfirmResult) => void, again: () => void) => ReactNode;
  summary: (result: AiConfirmResult, draft: D, again: () => void) => ReactNode;
}

/**
 * Photo → draft → the person's decision (11 §2): pick a photo, the server reads it into a draft
 * (polled while `processing`), the person confirms or discards, then a summary. Nothing is written
 * before the confirm; the server holds that rule, this only never sends one on its own.
 */
export function ScanFlow<D extends Draft>({ store, features, text, feature, create, get, ready, summary }: ScanFlowProps<D>) {
  const intl = useIntl();
  const [draftId, setDraftId] = useState<string | null>(null);
  const action = useAction();
  const [result, setResult] = useState<AiConfirmResult | null>(null);
  const draft = useDraft<D>(store, feature, draftId, get);

  const start = async (photo: File) => {
    // One key per photo: a retry after a dropped connection reuses it, so it never makes a second draft.
    const made = await action.run(`create:${photo.name}:${photo.size}:${photo.lastModified}`, (key) => create(photo, key));
    if (made) setDraftId(made.draftId);
  };
  const again = () => {
    setDraftId(null);
    setResult(null);
    action.clearError();
  };
  const tryAgain = (
    <div className={styles.actions}>
      <Button variant="primary" onClick={again}>
        {intl.formatMessage({ id: 'ai.again' })}
      </Button>
    </div>
  );

  if (draftId === null) {
    return (
      <>
        <p className={styles.sub}>{intl.formatMessage({ id: text.desc })}</p>
        <PhotoPick label={intl.formatMessage({ id: text.pick })} types={features.photoTypes} maxBytes={features.maxPhotoBytes} busy={action.pending} onPick={(f) => void start(f)} />
        <AiError error={action.error} />
      </>
    );
  }
  if (draft.isError) {
    return (
      <>
        <AiError error={draft.error} />
        {tryAgain}
      </>
    );
  }
  const d = draft.data;
  if (result && d) return summary(result, d, again);
  if (!d || d.status === 'processing') {
    return (
      <div role="status" aria-label={intl.formatMessage({ id: text.reading })}>
        <p className={styles.sub}>{intl.formatMessage({ id: text.reading })}</p>
        <Skeleton height="8rem" />
      </div>
    );
  }
  if (d.status === 'failed') {
    return (
      <>
        <Banner tone="warning">
          <span role="alert">{intl.formatMessage({ id: text.failed })}</span>
        </Banner>
        {tryAgain}
      </>
    );
  }
  if (d.status !== 'ready') {
    return (
      <>
        <Banner>{intl.formatMessage({ id: 'ai.closed' })}</Banner>
        {tryAgain}
      </>
    );
  }
  return ready(d, setResult, again);
}
