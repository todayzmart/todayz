import { confirmBusinessDocument, createBusinessDocument } from '@todayz/api-client/gen/business/business';
import type { BusinessDocument } from '@todayz/api-client/gen/model/businessDocument';
import type { DocumentKind } from '@todayz/api-client/gen/model/documentKind';
import { Skeleton, StatusPill, useToast, type PillTone } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { useRef, useState } from 'react';
import { useIntl } from 'react-intl';
import { BizError } from '../stores/BizError';
import { docsKey, useAction, useDocuments } from '../stores/queries';
import styles from '../stores/stores.module.css';
import { checkFile, contentTypeOf, DOCUMENT_KINDS, type FileProblem } from './documents';

const TONE: Record<BusinessDocument['status'], PillTone> = { pending: 'pending', uploaded: 'ready', accepted: 'accepted', rejected: 'rejected' };

/**
 * B-02 documents: one row per kind, a file picker each. The file goes straight to storage on a
 * presigned PUT (D-46), then the confirm call checks the bucket holds it and accepts it at once
 * (D-269: nothing waits for a review; the daily shop check looks at it later).
 */
export function DocumentsStep({
  bid,
  editable,
  kinds = DOCUMENT_KINDS,
  help = 'onboarding.docs.help',
}: {
  bid: string;
  editable: boolean;
  kinds?: readonly DocumentKind[];
  /** The note under the rows (an i18n id). */
  help?: string;
}) {
  const intl = useIntl();
  const docs = useDocuments(bid);
  return (
    <div className={styles.stack}>
      {docs.isLoading && <Skeleton height="4rem" />}
      {docs.isError && <BizError error={docs.error} />}
      {docs.data && (
        <ul className={styles.list}>
          {kinds.map((k) => (
            <KindRow key={k} bid={bid} kind={k} items={docs.data.items.filter((d) => d.kind === k)} editable={editable} />
          ))}
        </ul>
      )}
      <p className={styles.note}>{intl.formatMessage({ id: help })}</p>
    </div>
  );
}

function KindRow({ bid, kind, items, editable }: { bid: string; kind: DocumentKind; items: BusinessDocument[]; editable: boolean }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const action = useAction();
  const input = useRef<HTMLInputElement>(null);
  const [problem, setProblem] = useState<FileProblem | null>(null);
  const name = intl.formatMessage({ id: `onboarding.docs.kind.${kind}` });

  const pick = async (file: File | undefined) => {
    if (!file) return;
    const bad = checkFile(file);
    setProblem(bad);
    if (bad) return;
    const out = await action.run(`doc-${kind}`, async (key) => {
      const made = await createBusinessDocument(bid, { kind, contentType: contentTypeOf(file), sizeBytes: file.size }, { idempotencyKey: key });
      // The URL signs the type and size; the browser sets Content-Length itself from the body.
      const res = await fetch(made.upload.url, { method: made.upload.method, headers: { 'Content-Type': made.upload.headers['Content-Type'] }, body: file });
      if (!res.ok) throw new UploadError();
      return confirmBusinessDocument(bid, made.document.id);
    });
    if (input.current) input.current.value = '';
    await qc.invalidateQueries({ queryKey: docsKey(bid) });
    if (out) toast.show({ message: intl.formatMessage({ id: 'onboarding.docs.added' }, { name }) });
  };

  const failed = action.error instanceof UploadError;
  return (
    <li className={styles.stack}>
      <div className={styles.row}>
        <div className={styles.rowMain}>
          <span className={styles.rowName}>{name}</span>
          {items.map((d) => (
            <span key={d.id} className={styles.meta}>
              <StatusPill tone={TONE[d.status]}>{intl.formatMessage({ id: `onboarding.docs.status.${d.status}` })}</StatusPill>
              {d.status === 'rejected' && d.rejectReason ? ` ${d.rejectReason}` : ''}
            </span>
          ))}
        </div>
        {editable && (
          <div>
            <input
              ref={input}
              id={`doc-${kind}`}
              type="file"
              accept="image/jpeg,image/png,image/webp,application/pdf"
              className={styles.fileInput}
              disabled={action.pending}
              onChange={(e) => void pick(e.target.files?.[0])}
            />
            <label htmlFor={`doc-${kind}`} className="tz-btn">
              {intl.formatMessage({ id: 'onboarding.docs.add' }, { name })}
            </label>
          </div>
        )}
      </div>
      {problem && (
        <p role="alert" className="tz-field-error">
          {intl.formatMessage({ id: `onboarding.docs.err.${problem}` })}
        </p>
      )}
      {failed ? (
        <p role="alert" className="tz-field-error">
          {intl.formatMessage({ id: 'onboarding.docs.uploadFailed' })}
        </p>
      ) : (
        <BizError error={action.error} />
      )}
    </li>
  );
}

class UploadError extends Error {}
