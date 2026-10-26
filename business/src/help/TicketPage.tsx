import { ApiError } from '@todayz/api-client';
import type { BizTicketMessage } from '@todayz/api-client/gen/model/bizTicketMessage';
import type { BizTicketThread } from '@todayz/api-client/gen/model/bizTicketThread';
import { Banner, Button, EmptyState, Skeleton, StatusPill } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { ArrowLeft, Inbox, SearchX } from 'lucide-react';
import { useId, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { whenText } from '../stock/labels';
import { useAction, useCurrentBusiness } from '../stores/queries';
import { AttachmentLinks, AttachmentPicker } from './Attachments';
import { useAttachments } from './useAttachments';
import { STATUS_TONE } from './labels';
import { BODY_MAX } from './limits';
import { canTickets } from './permissions';
import { helpErrorId, sendMessage, useThread } from './queries';
import styles from './help.module.css';

/** B-15 ticket: the thread with support and the reply box. */
export function TicketPage({ ticketId }: { ticketId: string }) {
  const intl = useIntl();
  const current = useCurrentBusiness();
  return (
    <div className={styles.page}>
      <Link to="/help" search={{ tab: 'tickets' }} className={`tz-btn tz-btn-ghost ${styles.back}`}>
        <ArrowLeft className="tz-ico tz-ico-sm" aria-hidden="true" />
        {intl.formatMessage({ id: 'help.back' })}
      </Link>
      {current.status === 'loading' && (
        <div role="status" aria-label={intl.formatMessage({ id: 'help.loading' })}>
          <Skeleton height="12rem" />
        </div>
      )}
      {current.status === 'error' && (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'help.error.title' })}
          description={intl.formatMessage({ id: 'help.error.body' })}
          action={<Button loading={current.retrying} onClick={current.retry}>{intl.formatMessage({ id: 'help.error.retry' })}</Button>}
        />
      )}
      {current.status === 'none' && <Banner tone="info">{intl.formatMessage({ id: 'help.noBusiness' })}</Banner>}
      {current.status === 'ready' &&
        (canTickets(current.business.role) ? (
          <Thread bid={current.business.id} ticketId={ticketId} />
        ) : (
          <Banner tone="info">{intl.formatMessage({ id: 'help.askManager' })}</Banner>
        ))}
    </div>
  );
}

function Thread({ bid, ticketId }: { bid: string; ticketId: string }) {
  const intl = useIntl();
  const q = useThread(bid, ticketId);
  if (q.isPending) {
    return (
      <div role="status" aria-label={intl.formatMessage({ id: 'help.ticket.loading' })}>
        <Skeleton height="12rem" />
      </div>
    );
  }
  if (q.isError) {
    const gone = q.error instanceof ApiError && (q.error.status === 404 || q.error.status === 403);
    return gone ? (
      <EmptyState
        icon={SearchX}
        title={intl.formatMessage({ id: 'help.ticket.notFound.title' })}
        description={intl.formatMessage({ id: 'help.ticket.notFound.body' })}
        action={
          <Link to="/help" search={{ tab: 'tickets' }} className={`tz-btn tz-btn-primary ${styles.linkBtn}`}>
            {intl.formatMessage({ id: 'help.ticket.notFound.action' })}
          </Link>
        }
      />
    ) : (
      <EmptyState
        icon={Inbox}
        title={intl.formatMessage({ id: 'help.ticket.error.title' })}
        description={intl.formatMessage({ id: 'help.error.body' })}
        action={<Button loading={q.isFetching} onClick={() => void q.refetch()}>{intl.formatMessage({ id: 'help.error.retry' })}</Button>}
      />
    );
  }
  const { ticket: t, messages, serverNow } = q.data;
  const now = Date.parse(serverNow);
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
          <MessageItem key={m.id} message={m} now={now} />
        ))}
      </ol>
      {t.status === 'closed' ? (
        <div className={styles.actions}>
          <Link to="/help/new" className={`tz-btn tz-btn-primary ${styles.linkBtn}`}>
            {intl.formatMessage({ id: 'help.ticket.newInstead' })}
          </Link>
        </div>
      ) : (
        <ReplyBox bid={bid} thread={q.data} />
      )}
    </>
  );
}

function MessageItem({ message: m, now }: { message: BizTicketMessage; now: number }) {
  const intl = useIntl();
  // Support replies carry no agent name (D-226): the shop sees "TodayZ support".
  const who = m.from === 'support' ? intl.formatMessage({ id: 'help.ticket.fromSupport' }) : (m.authorName ?? intl.formatMessage({ id: 'help.ticket.fromYou' }));
  return (
    <li className={styles.message} data-from={m.from}>
      <p className={styles.messageHead}>
        <b>{who}</b>
        <span>{whenText(intl, m.createdAt, now)}</span>
      </p>
      <p className={styles.messageBody}>{m.body}</p>
      <AttachmentLinks files={m.attachments} />
    </li>
  );
}

function ReplyBox({ bid, thread }: { bid: string; thread: BizTicketThread }) {
  const intl = useIntl();
  const id = useId();
  const qc = useQueryClient();
  const action = useAction();
  const att = useAttachments(bid);
  const [body, setBody] = useState('');
  const [touched, setTouched] = useState(false);
  const clean = body.trim();
  const invalid = clean.length === 0 || clean.length > BODY_MAX;
  const reopens = thread.ticket.status === 'waiting' || thread.ticket.status === 'resolved';

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (invalid || action.pending || att.uploading) return;
    const req = { body: clean, ...(att.files.length ? { attachmentIds: att.files.map((f) => f.id) } : {}) };
    const out = await action.run(`reply-${thread.ticket.id}`, (key) => sendMessage(qc, bid, thread.ticket.id, req, key));
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
      {action.error !== null && (
        <p role="alert" className="tz-field-error">
          {intl.formatMessage({ id: helpErrorId(action.error) })}
        </p>
      )}
      <div className={styles.actions}>
        <Button type="submit" variant="primary" loading={action.pending} loadingLabel={intl.formatMessage({ id: 'help.sending' })}>
          {intl.formatMessage({ id: 'help.reply.send' })}
        </Button>
      </div>
    </form>
  );
}
