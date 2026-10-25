import { ApiError } from '@todayz/api-client';
import type { MyTicketCategory } from '@todayz/api-client/gen/model/myTicketCategory';
import type { MyTicketCreate } from '@todayz/api-client/gen/model/myTicketCreate';
import { formatMoney } from '@todayz/i18n';
import { Button, TextField, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { ArrowLeft } from 'lucide-react';
import { useId, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { AttachmentPicker } from './Attachments';
import { BODY_MAX, CATEGORIES, SUBJECT_MAX, SUBJECT_MIN } from './limits';
import { helpErrorId, openTicket, useRecentOrders } from './queries';
import { SignedInOnly } from './SignedInOnly';
import { useAttachments } from './useAttachments';
import { useSend } from './useSend';
import styles from './help.module.css';

/** C-19 new ticket: a topic, an optional order (preset when opened from C-12), the message and files. */
export function NewTicketPage({ orderId }: { orderId?: string }) {
  const intl = useIntl();
  return (
    <SignedInOnly next={orderId ? `/help/new?orderId=${orderId}` : '/help/new'}>
      <div className={styles.page}>
        <Link to="/help" className={`tz-btn tz-btn-ghost ${styles.back}`}>
          <ArrowLeft className="tz-ico tz-ico-sm" aria-hidden="true" />
          {intl.formatMessage({ id: 'help.back' })}
        </Link>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'help.new.title' })}</h1>
        <TicketForm orderId={orderId} />
      </div>
    </SignedInOnly>
  );
}

function TicketForm({ orderId: preset }: { orderId?: string | undefined }) {
  const intl = useIntl();
  const id = useId();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const toast = useToast();
  const send = useSend();
  const att = useAttachments();
  const orders = useRecentOrders();
  const [category, setCategory] = useState<MyTicketCategory | ''>(preset ? 'order' : '');
  const [subject, setSubject] = useState('');
  const [orderId, setOrderId] = useState(preset ?? '');
  const [body, setBody] = useState('');
  const [touched, setTouched] = useState(false);

  const cleanSubject = subject.trim();
  const cleanBody = body.trim();
  const errors = {
    category: category === '' ? intl.formatMessage({ id: 'help.new.category.required' }) : undefined,
    subject:
      cleanSubject.length < SUBJECT_MIN || cleanSubject.length > SUBJECT_MAX
        ? intl.formatMessage({ id: 'help.new.subject.required' }, { min: SUBJECT_MIN, max: SUBJECT_MAX })
        : undefined,
    body: cleanBody.length === 0 || cleanBody.length > BODY_MAX ? intl.formatMessage({ id: 'help.new.body.required' }) : undefined,
  };
  const invalid = Boolean(errors.category ?? errors.subject ?? errors.body);
  const fields = send.error instanceof ApiError && send.error.code === 'validation_failed' ? send.error.fields : {};
  const orderRefused = Object.keys(fields).some((k) => k.endsWith('orderId'));
  // The preset order stays listed while the recent orders load, or when it is older than they go.
  const options = orders.data ?? [];
  const presetMissing = preset !== undefined && !options.some((o) => o.id === preset);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (invalid || send.pending || att.uploading || category === '') return;
    const req: MyTicketCreate = {
      category,
      subject: cleanSubject,
      body: cleanBody,
      ...(orderId ? { orderId } : {}),
      ...(att.files.length ? { attachmentIds: att.files.map((f) => f.id) } : {}),
    };
    const out = await send.run((key) => openTicket(qc, req, key));
    if (!out) return;
    toast.show({ message: intl.formatMessage({ id: 'help.new.sent' }, { number: out.ticket.number }) });
    void navigate({ to: '/help/$ticketId', params: { ticketId: out.ticket.id }, replace: true });
  };

  return (
    <form className={`tz-card ${styles.form}`} onSubmit={(e) => void submit(e)} noValidate>
      <p className={styles.note}>{intl.formatMessage({ id: 'help.new.intro' })}</p>
      <div className="tz-field">
        <label className="tz-field-label" htmlFor={`${id}-category`}>
          {intl.formatMessage({ id: 'help.new.category' })}
        </label>
        <select
          id={`${id}-category`}
          className="tz-input"
          value={category}
          aria-invalid={touched && Boolean(errors.category)}
          aria-describedby={touched && errors.category ? `${id}-category-error` : undefined}
          onChange={(e) => setCategory(e.target.value as MyTicketCategory | '')}
        >
          <option value="">{intl.formatMessage({ id: 'help.new.category.pick' })}</option>
          {CATEGORIES.map((c) => (
            <option key={c} value={c}>
              {intl.formatMessage({ id: `help.category.${c}` })}
            </option>
          ))}
        </select>
        {touched && errors.category && (
          <p id={`${id}-category-error`} className="tz-field-error">
            {errors.category}
          </p>
        )}
      </div>
      <div className="tz-field">
        <label className="tz-field-label" htmlFor={`${id}-order`}>
          {intl.formatMessage({ id: 'help.new.order' })}
        </label>
        <select
          id={`${id}-order`}
          className="tz-input"
          value={orderId}
          aria-invalid={orderRefused}
          aria-describedby={`${id}-order-help`}
          onChange={(e) => setOrderId(e.target.value)}
        >
          <option value="">{intl.formatMessage({ id: 'help.new.order.none' })}</option>
          {presetMissing && <option value={preset}>{intl.formatMessage({ id: 'help.new.order.this' })}</option>}
          {options.map((o) => (
            <option key={o.id} value={o.id}>
              {[o.number, o.store.name, formatMoney(o.totalPaise)].join(' · ')}
            </option>
          ))}
        </select>
        <p id={`${id}-order-help`} className={orderRefused ? 'tz-field-error' : 'tz-field-help'}>
          {intl.formatMessage({
            id: orderRefused ? 'help.new.order.refused' : orders.isError ? 'help.new.order.failed' : 'help.new.order.help',
          })}
        </p>
      </div>
      <TextField
        label={intl.formatMessage({ id: 'help.new.subject' })}
        help={intl.formatMessage({ id: 'help.new.subject.help' })}
        error={touched ? errors.subject : undefined}
        value={subject}
        maxLength={SUBJECT_MAX}
        onChange={(e) => setSubject(e.target.value)}
      />
      <div className="tz-field">
        <label className="tz-field-label" htmlFor={`${id}-body`}>
          {intl.formatMessage({ id: 'help.new.body' })}
        </label>
        <textarea
          id={`${id}-body`}
          className="tz-input"
          rows={6}
          maxLength={BODY_MAX}
          value={body}
          aria-invalid={touched && Boolean(errors.body)}
          aria-describedby={`${id}-body-help`}
          onChange={(e) => setBody(e.target.value)}
        />
        <p id={`${id}-body-help`} className={touched && errors.body ? 'tz-field-error' : 'tz-field-help'}>
          {touched && errors.body ? errors.body : intl.formatMessage({ id: 'help.new.body.help' })}
        </p>
      </div>
      <AttachmentPicker id={`${id}-files`} att={att} />
      {send.error !== null && (
        <p role="alert" className="tz-field-error">
          {intl.formatMessage({ id: helpErrorId(send.error) })}
        </p>
      )}
      <div className={styles.actions}>
        <Button type="submit" variant="primary" block loading={send.pending} loadingLabel={intl.formatMessage({ id: 'help.sending' })}>
          {intl.formatMessage({ id: 'help.new.send' })}
        </Button>
      </div>
    </form>
  );
}
