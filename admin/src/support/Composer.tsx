import type { TicketAttachment } from '@todayz/api-client/gen/model/ticketAttachment';
import type { TicketMessageRequest } from '@todayz/api-client/gen/model/ticketMessageRequest';
import { Button, Segmented } from '@todayz/ui';
import { Paperclip, X } from 'lucide-react';
import { useId, useRef, useState, type ChangeEvent, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { useProblemText } from '../app/problems';
import { ACCEPT, MAX_BYTES, MAX_FILES, useMessage, useUpload } from './queries';
import css from './support.module.css';

/** The contract bound on TicketMessageRequest.body. */
export const MAX_BODY = 4000;

type Kind = 'reply' | 'note';

/**
 * Reply to the opener or write an internal note, with up to five files. Each file goes straight to
 * storage on a presigned PUT as soon as it is picked; the message then claims the uploads by id. A
 * closed ticket takes notes only.
 */
export function Composer({ ticketId, closed }: { ticketId: string; closed: boolean }) {
  const intl = useIntl();
  const id = useId();
  const problemText = useProblemText();
  const [kind, setKind] = useState<Kind>(closed ? 'note' : 'reply');
  const [body, setBody] = useState('');
  const [files, setFiles] = useState<TicketAttachment[]>([]);
  const [fileError, setFileError] = useState<string | null>(null);
  const [touched, setTouched] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const reply = useMessage(ticketId, 'reply');
  const note = useMessage(ticketId, 'note');
  const upload = useUpload(ticketId);
  const send = kind === 'reply' && !closed ? reply : note;
  const mode: Kind = closed ? 'note' : kind;
  const clean = body.trim();
  const invalid = clean.length === 0 || clean.length > MAX_BODY;

  const pick = async (e: ChangeEvent<HTMLInputElement>) => {
    const chosen = Array.from(e.target.files ?? []);
    e.target.value = '';
    setFileError(null);
    for (const f of chosen) {
      if (files.length >= MAX_FILES) {
        setFileError(intl.formatMessage({ id: 'support.files.tooMany' }, { max: MAX_FILES }));
        return;
      }
      if (!(ACCEPT as readonly string[]).includes(f.type)) {
        setFileError(intl.formatMessage({ id: 'support.files.type' }, { name: f.name }));
        continue;
      }
      if (f.size < 1 || f.size > MAX_BYTES) {
        setFileError(intl.formatMessage({ id: 'support.files.size' }, { name: f.name }));
        continue;
      }
      try {
        const a = await upload.mutateAsync(f);
        setFiles((list) => [...list, a]);
      } catch {
        setFileError(intl.formatMessage({ id: 'support.files.failed' }, { name: f.name }));
      }
    }
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (invalid || send.isPending || upload.isPending) return;
    const req: TicketMessageRequest = { body: clean, ...(files.length ? { attachmentIds: files.map((f) => f.id) } : {}) };
    send.mutate(req, {
      onSuccess: () => {
        setBody('');
        setFiles([]);
        setTouched(false);
        setFileError(null);
      },
    });
  };

  return (
    <form className={`tz-card ${css.composer}`} onSubmit={submit} noValidate aria-labelledby={`${id}-title`}>
      <h2 id={`${id}-title`} className="tz-card-title">
        {intl.formatMessage({ id: 'support.compose.title' })}
      </h2>
      {!closed && (
        <Segmented<Kind>
          label={intl.formatMessage({ id: 'support.compose.kind' })}
          options={[
            { value: 'reply', label: intl.formatMessage({ id: 'support.compose.reply' }) },
            { value: 'note', label: intl.formatMessage({ id: 'support.compose.note' }) },
          ]}
          value={kind}
          onChange={setKind}
        />
      )}
      <div className="tz-field">
        <label className="tz-field-label" htmlFor={`${id}-body`}>
          {mode === 'reply' ? intl.formatMessage({ id: 'support.compose.replyLabel' }) : intl.formatMessage({ id: 'support.compose.noteLabel' })}
        </label>
        <textarea
          id={`${id}-body`}
          className="tz-input"
          rows={5}
          maxLength={MAX_BODY}
          value={body}
          aria-invalid={touched && invalid}
          aria-describedby={`${id}-help`}
          onChange={(e) => setBody(e.target.value)}
        />
        <p id={`${id}-help`} className={touched && invalid ? 'tz-field-error' : 'tz-field-help'}>
          {touched && invalid
            ? intl.formatMessage({ id: 'support.compose.required' })
            : mode === 'reply'
              ? intl.formatMessage({ id: 'support.compose.replyHelp' })
              : intl.formatMessage({ id: 'support.compose.noteHelp' })}
        </p>
      </div>
      {files.length > 0 && (
        <ul className={css.files} aria-label={intl.formatMessage({ id: 'support.files.list' })}>
          {files.map((f) => (
            <li key={f.id}>
              <span>{f.fileName}</span>
              <Button
                size="sm"
                variant="ghost"
                icon={X}
                aria-label={intl.formatMessage({ id: 'support.files.remove' }, { name: f.fileName })}
                onClick={() => setFiles((list) => list.filter((x) => x.id !== f.id))}
              />
            </li>
          ))}
        </ul>
      )}
      {fileError && (
        <p role="alert" className="tz-field-error">
          {fileError}
        </p>
      )}
      {send.error && (
        <p role="alert" className="tz-field-error">
          {problemText(send.error)}
        </p>
      )}
      <div className={css.composerActions}>
        <input
          ref={input}
          id={`${id}-files`}
          type="file"
          className={css.fileInput}
          accept={ACCEPT.join(',')}
          multiple
          onChange={(e) => void pick(e)}
        />
        <Button
          variant="ghost"
          icon={Paperclip}
          loading={upload.isPending}
          loadingLabel={intl.formatMessage({ id: 'support.files.uploading' })}
          disabled={files.length >= MAX_FILES}
          onClick={() => input.current?.click()}
        >
          {intl.formatMessage({ id: 'support.files.add' })}
        </Button>
        <Button type="submit" variant="primary" loading={send.isPending} loadingLabel={intl.formatMessage({ id: 'common.saving' })}>
          {mode === 'reply' ? intl.formatMessage({ id: 'support.compose.sendReply' }) : intl.formatMessage({ id: 'support.compose.addNote' })}
        </Button>
      </div>
    </form>
  );
}
