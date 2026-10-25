import type { MyTicketAttachment } from '@todayz/api-client/gen/model/myTicketAttachment';
import { Button } from '@todayz/ui';
import { FileText, Paperclip, X } from 'lucide-react';
import { useRef } from 'react';
import { useIntl } from 'react-intl';
import { ACCEPT, MAX_BYTES, MAX_FILES } from './limits';
import type { Attachments } from './useAttachments';
import styles from './help.module.css';

/** The "Add photo or PDF" button and the files picked so far. */
export function AttachmentPicker({ id, att }: { id: string; att: Attachments }) {
  const intl = useIntl();
  const input = useRef<HTMLInputElement>(null);
  return (
    <div className={styles.files}>
      {att.files.length > 0 && (
        <ul className={styles.fileList} aria-label={intl.formatMessage({ id: 'help.files.list' })}>
          {att.files.map((f) => (
            <li key={f.id}>
              <FileText className="tz-ico tz-ico-sm" aria-hidden="true" />
              <span className={styles.fileName}>{f.fileName}</span>
              <Button
                size="sm"
                variant="ghost"
                icon={X}
                aria-label={intl.formatMessage({ id: 'help.files.remove' }, { name: f.fileName })}
                onClick={() => att.remove(f.id)}
              />
            </li>
          ))}
        </ul>
      )}
      {att.error && (
        <p role="alert" className="tz-field-error">
          {att.error}
        </p>
      )}
      <input
        ref={input}
        id={id}
        type="file"
        className={styles.fileInput}
        tabIndex={-1}
        aria-hidden="true"
        accept={ACCEPT.join(',')}
        multiple
        onChange={(e) => void att.pick(e)}
      />
      <Button
        variant="ghost"
        icon={Paperclip}
        loading={att.uploading}
        loadingLabel={intl.formatMessage({ id: 'help.files.uploading' })}
        disabled={att.files.length >= MAX_FILES}
        onClick={() => input.current?.click()}
      >
        {intl.formatMessage({ id: 'help.files.add' })}
      </Button>
      <p className="tz-field-help">{intl.formatMessage({ id: 'help.files.help' }, { max: MAX_FILES, mb: MAX_BYTES / 1024 / 1024 })}</p>
    </div>
  );
}

/** The files on a message, each a link while storage gives one. */
export function AttachmentLinks({ files }: { files: readonly MyTicketAttachment[] }) {
  const intl = useIntl();
  if (files.length === 0) return null;
  return (
    <ul className={styles.fileList} aria-label={intl.formatMessage({ id: 'help.files.list' })}>
      {files.map((f) => (
        <li key={f.id}>
          <FileText className="tz-ico tz-ico-sm" aria-hidden="true" />
          {f.url ? (
            <a href={f.url} target="_blank" rel="noopener noreferrer" className={styles.fileName}>
              {f.fileName}
            </a>
          ) : (
            <span className={styles.fileName}>{f.fileName}</span>
          )}
        </li>
      ))}
    </ul>
  );
}
