import { ApiError } from '@todayz/api-client';
import type { MyTicketMessage } from '@todayz/api-client/gen/model/myTicketMessage';
import type { MyTicketThread } from '@todayz/api-client/gen/model/myTicketThread';
import { Button, EmptyState, Skeleton, StatusPill } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { ArrowLeft, Inbox, SearchX } from 'lucide-react';
import { useId, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { useFmt } from '../home/format';
import { AttachmentLinks, AttachmentPicker } from './Attachments';
import { STATUS_TONE } from './labels';
import { BODY_MAX } from './limits';
import { helpErrorKey, sendMessage, useThread } from './queries';
import { SignedInOnly } from './SignedInOnly';
import { useAttachments } from './useAttachments';
import { useSend } from './useSend';
import styles from './help.module.css';

/** C-19 ticket: the thread with support and the reply box. */
export function TicketPage({ ticketId }: { ticketId: string }) {
  const intl = useIntl();
  return (
    <SignedInOnly next={`/help/${ticketId}`}>
      <div className={styles.page}>
        <Link to="/help" className={`tz-btn tz-btn-ghost ${styles.back}`}>
          <ArrowLeft className="tz-ico tz-ico-sm" aria-hidden="true" />
          {intl.formatMessage({ id: 'help.back' })}
        </Link>
        <Thread ticketId={ticketId} />
      </div>
    </SignedInOnly>
  );
}

function Thread({ ticketId }: { ticketId: string }) {
  const intl = useIntl();
  const q = useThread(ticketId);
  if (q.isPending) {
    return (
      <div role="status" aria-label={intl.formatMessage({ id: 'help.ticket.loading' })}>
        <Skeleton height="12rem" />
      </div>
    );
  }
  if (q.isError) {
    const gone = q.error instanceof ApiError && q.error.status === 404;
    return gone ? (
      <EmptyState
        icon={SearchX}
        title={intl.formatMessage({ id: 'help.ticket.notFound.title' })}
        description={intl.formatMessage({ id: 'help.ticket.notFound.body' })}
        action={
          <Link to="/help" className={`tz-btn tz-btn-primary ${styles.linkBtn}`}>
            {intl.formatMessage({ id: 'help.ticket.notFound.action' })}
          </Link>
        }
      />
    ) : (
      <EmptyState
        icon={Inbox}
        title={intl.formatMessage({ id: 'help.ticket.error.title' })}
        description={intl.formatMessage({ id: 'help.error.body' })}
        action={<Button onClick={() => void q.refetch()}>{intl.formatMessage({ id: 'help.error.retry' })}</Button>}
      />
    );
  }
  const { ticket: t, messages, serverNow } = q.data;
  return (
    <>
      <header className={styles.head}>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'help.ticket.title' }, { number: t.number })}</h1>
        <StatusPill tone={STATUS_TONE[t.status]}>{intl.formatMessage({ id: `help.status.${t.status}` })}</StatusPill>
      </header>
      <div className={styles.summary}>
        <p className={styles.subject}>{t.subject}</p>
        <p className={styles.meta}>
          {[intl.formatMessage({ id: `help.category.${t.category}` }), t.storeName, t.orderNumber].filter(Boolean).join(' · ')}
        </p>
        <p className={styles.note}>{intl.formatMessage({ id: `help.ticket.state.${t.status}` })}</p>
      </div>
      <ol className={styles.thread} aria-label={intl.formatMessage({ id: 'help.ticket.messages' })}>
        {messages.map((m) => (
          <MessageItem key={m.id} message={m} serverNow={serverNow} />
        ))}
      </ol>
      {t.status === 'closed' ? (
        <div className={styles.actions}>
          <Link to="/help/new" className={`tz-btn tz-btn-primary ${styles.linkBtn}`}>
            {intl.formatMessage({ id: 'help.ticket.newInstead' })}
          </Link>
        </div>
      ) : (
        <ReplyBox thread={q.data} />
      )}
    </>
  );
}

function MessageItem({ message: m, serverNow }: { message: MyTicketMessage; serverNow: string }) {
  const intl = useIntl();
  const { ago } = useFmt();
  // Support replies carry no agent name (D-226).
  const who = intl.formatMessage({ id: m.from === 'support' ? 'help.ticket.fromSupport' : 'help.ticket.fromYou' });
  return (
    <li className={styles.message} data-from={m.from}>
      <p className={styles.messageHead}>
        <b>{who}</b>
        <span>{ago(m.createdAt, serverNow)}</span>
      </p>
      <p className={styles.messageBody}>{m.body}</p>
      <AttachmentLinks files={m.attachments} />
    </li>
  );
}

function ReplyBox({ thread }: { thread: MyTicketThread }) {
  const intl = useIntl();
  const id = useId();
  const qc = useQueryClient();
  const send = useSend();
  const att = useAttachments();
  const [body, setBody] = useState('');
  const [touched, setTouched] = useState(false);
  const clean = body.trim();
  const invalid = clean.length === 0 || clean.length > BODY_MAX;
  const reopens = thread.ticket.status === 'waiting' || thread.ticket.status === 'resolved';

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (invalid || send.pending || att.uploading) return;
    const req = { body: clean, ...(att.files.length ? { attachmentIds: att.files.map((f) => f.id) } : {}) };
    const out = await send.run((key) => sendMessage(qc, thread.ticket.id, req, key));
    if (!out) return;
    setBody('');
    setTouched(false);
    att.clear();
  };

  return (
    <form className={`tz-card ${styles.form}`} onSubmit={(e) => void submit(e)} noValidate aria-labelledby={`${id}-title`}>
      <h2 id={`${id}-title`} className="tz-card-title">
        {intl.formatMessage({ id: 'help.reply.title' })}
      </h2>
      <div className="tz-field">
        <label className="tz-field-label" htmlFor={`${id}-body`}>
          {intl.formatMessage({ id: 'help.reply.label' })}
        </label>
        <textarea
          id={`${id}-body`}
          className="tz-input"
          rows={4}
          maxLength={BODY_MAX}
          value={body}
          aria-invalid={touched && invalid}
          aria-describedby={`${id}-help`}
          onChange={(e) => setBody(e.target.value)}
        />
        <p id={`${id}-help`} className={touched && invalid ? 'tz-field-error' : 'tz-field-help'}>
          {touched && invalid
            ? intl.formatMessage({ id: 'help.new.body.required' })
            : intl.formatMessage({ id: reopens ? 'help.reply.reopens' : 'help.reply.help' })}
        </p>
      </div>
      <AttachmentPicker id={`${id}-files`} att={att} />
      {send.error !== null && (
        <p role="alert" className="tz-field-error">
          {intl.formatMessage({ id: `help.error.${helpErrorKey(send.error)}` })}
        </p>
      )}
      <div className={styles.actions}>
        <Button type="submit" variant="primary" loading={send.pending} loadingLabel={intl.formatMessage({ id: 'help.sending' })}>
          {intl.formatMessage({ id: 'help.reply.send' })}
        </Button>
      </div>
    </form>
  );
}
