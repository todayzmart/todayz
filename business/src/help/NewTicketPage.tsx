import { ApiError } from '@todayz/api-client';
import type { BizTicketCategory } from '@todayz/api-client/gen/model/bizTicketCategory';
import type { BizTicketCreate } from '@todayz/api-client/gen/model/bizTicketCreate';
import type { BusinessSummary } from '@todayz/api-client/gen/model/businessSummary';
import { formatMoney } from '@todayz/i18n';
import { Banner, Button, EmptyState, Skeleton, TextField, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { ArrowLeft, Inbox } from 'lucide-react';
import { useId, useState, type FormEvent } from 'react';
import { useIntl } from 'react-intl';
import { rememberedStore } from '../app/shop';
import { whenText } from '../stock/labels';
import { useAction, useCurrentBusiness } from '../stores/queries';
import { AttachmentPicker } from './Attachments';
import { useAttachments } from './useAttachments';
import { BODY_MAX, CATEGORIES, SUBJECT_MAX, SUBJECT_MIN } from './limits';
import { canTickets } from './permissions';
import { helpErrorId, openTicket, useRecentOrders } from './queries';
import styles from './help.module.css';

/** B-15 new ticket: for the owner and manager; a cashier is told to ask one of them. */
export function NewTicketPage() {
  const intl = useIntl();
  const current = useCurrentBusiness();
  return (
    <div className={styles.page}>
      <Link to="/help" search={{ tab: 'tickets' }} className={`tz-btn tz-btn-ghost ${styles.back}`}>
        <ArrowLeft className="tz-ico tz-ico-sm" aria-hidden="true" />
        {intl.formatMessage({ id: 'help.back' })}
      </Link>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'help.new.title' })}</h1>
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
          action={<Button onClick={current.retry}>{intl.formatMessage({ id: 'help.error.retry' })}</Button>}
        />
      )}
      {current.status === 'none' && <Banner tone="info">{intl.formatMessage({ id: 'help.noBusiness' })}</Banner>}
      {current.status === 'ready' &&
        (canTickets(current.business.role) ? (
          <TicketForm business={current.business} />
        ) : (
          <Banner tone="info">{intl.formatMessage({ id: 'help.askManager' })}</Banner>
        ))}
    </div>
  );
}

/** The remembered store when it belongs to this business, else its only store, else none. */
function defaultStore(b: BusinessSummary): string {
  const remembered = rememberedStore();
  if (remembered && b.stores.some((s) => s.id === remembered)) return remembered;
  return b.stores.length === 1 ? (b.stores[0]?.id ?? '') : '';
}

function TicketForm({ business }: { business: BusinessSummary }) {
  const intl = useIntl();
  const id = useId();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const toast = useToast();
  const action = useAction();
  const att = useAttachments(business.id);
  const [category, setCategory] = useState<BizTicketCategory | ''>('');
  const [subject, setSubject] = useState('');
  const [storeId, setStoreId] = useState(() => defaultStore(business));
  const [orderId, setOrderId] = useState('');
  const [body, setBody] = useState('');
  const [touched, setTouched] = useState(false);
  const orders = useRecentOrders(business.id, storeId || null);

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
  const fields = action.error instanceof ApiError && action.error.code === 'validation_failed' ? action.error.fields : {};
  const fieldError = (name: string) => (Object.keys(fields).some((k) => k.endsWith(name)) ? intl.formatMessage({ id: 'help.new.fieldRefused' }) : undefined);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (invalid || action.pending || att.uploading || category === '') return;
    const req: BizTicketCreate = {
      category,
      subject: cleanSubject,
      body: cleanBody,
      ...(storeId ? { storeId } : {}),
      ...(orderId ? { orderId } : {}),
      ...(att.files.length ? { attachmentIds: att.files.map((f) => f.id) } : {}),
    };
    const out = await action.run('open-ticket', (key) => openTicket(qc, business.id, req, key));
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
          onChange={(e) => setCategory(e.target.value as BizTicketCategory | '')}
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
      <TextField
        label={intl.formatMessage({ id: 'help.new.subject' })}
        help={intl.formatMessage({ id: 'help.new.subject.help' })}
        error={touched ? errors.subject : undefined}
        value={subject}
        maxLength={SUBJECT_MAX}
        onChange={(e) => setSubject(e.target.value)}
      />
      {business.stores.length > 0 && (
        <div className="tz-field">
          <label className="tz-field-label" htmlFor={`${id}-store`}>
            {intl.formatMessage({ id: 'help.new.store' })}
          </label>
          <select
            id={`${id}-store`}
            className="tz-input"
            value={storeId}
            aria-invalid={Boolean(fieldError('storeId'))}
            onChange={(e) => {
              setStoreId(e.target.value);
              setOrderId('');
            }}
          >
            <option value="">{intl.formatMessage({ id: 'help.new.store.none' })}</option>
            {business.stores.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
          {fieldError('storeId') && <p className="tz-field-error">{fieldError('storeId')}</p>}
        </div>
      )}
      {storeId && (
        <div className="tz-field">
          <label className="tz-field-label" htmlFor={`${id}-order`}>
            {intl.formatMessage({ id: 'help.new.order' })}
          </label>
          <select
            id={`${id}-order`}
            className="tz-input"
            value={orderId}
            disabled={orders.isPending}
            aria-invalid={Boolean(fieldError('orderId'))}
            aria-describedby={`${id}-order-help`}
            onChange={(e) => setOrderId(e.target.value)}
          >
            <option value="">{intl.formatMessage({ id: 'help.new.order.none' })}</option>
            {(orders.data ?? []).map((o) => (
              <option key={o.id} value={o.id}>
                {[o.number, o.customerName, formatMoney(o.totalPaise, 'product'), whenText(intl, o.placedAt)].filter(Boolean).join(' · ')}
              </option>
            ))}
          </select>
          <p id={`${id}-order-help`} className={fieldError('orderId') ? 'tz-field-error' : 'tz-field-help'}>
            {fieldError('orderId') ??
              intl.formatMessage({ id: orders.isError ? 'help.new.order.failed' : 'help.new.order.help' })}
          </p>
        </div>
      )}
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
      {action.error !== null && (
        <p role="alert" className="tz-field-error">
          {intl.formatMessage({ id: helpErrorId(action.error) })}
        </p>
      )}
      <div className={styles.actions}>
        <Button type="submit" variant="primary" loading={action.pending} loadingLabel={intl.formatMessage({ id: 'help.sending' })}>
          {intl.formatMessage({ id: 'help.new.send' })}
        </Button>
      </div>
    </form>
  );
}
