import type { AdminBusiness } from '@todayz/api-client/gen/model/adminBusiness';
import type { AdminBusinessDocument } from '@todayz/api-client/gen/model/adminBusinessDocument';
import type { AdminBusinessStaff } from '@todayz/api-client/gen/model/adminBusinessStaff';
import type { AdminBusinessStore } from '@todayz/api-client/gen/model/adminBusinessStore';
import type { AdminInfoField } from '@todayz/api-client/gen/model/adminInfoField';
import type { AdminKycField } from '@todayz/api-client/gen/model/adminKycField';
import { ApiError } from '@todayz/api-client';
import { Button, DataTable, EmptyState, Skeleton, StatusPill, useToast, type ColumnDef } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { Ban, BadgeCheck, Building2, Eye, EyeOff, MessageSquareWarning, RotateCcw, X } from 'lucide-react';
import { Fragment, useCallback, useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { can, useRoles } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { ReasonDialog } from '../app/ReasonDialog';
import { AuditPanel } from '../audit/AuditPanel';
import { storeTone, useStoreLabels } from '../stores/labels';
import { INFO_FIELDS, docTone, kycTone, statusTone, useBusinessLabels } from './labels';
import { useBusiness, useBusinessMove, useReveal, type BusinessMove } from './queries';
import css from '../app/page.module.css';

/** A-03 business detail: profile, stores, staff, KYC (masked, reveal audited), documents; each move audited. */
export function BusinessPage({ id }: { id: string }) {
  const intl = useIntl();
  const q = useBusiness(id);
  if (q.isPending) return <Skeleton shape="block" height="240px" />;
  if (q.isError) {
    const missing = q.error instanceof ApiError && q.error.code === 'not_found';
    return (
      <EmptyState
        icon={Building2}
        headingLevel={2}
        title={intl.formatMessage({ id: missing ? 'businesses.missing' : 'businesses.loadFailed' })}
        action={
          missing ? (
            <Link to="/businesses" className="tz-btn tz-btn-primary">
              {intl.formatMessage({ id: 'businesses.back' })}
            </Link>
          ) : (
            <Button onClick={() => void q.refetch()}>{intl.formatMessage({ id: 'common.retry' })}</Button>
          )
        }
      />
    );
  }
  return <Detail b={q.data} />;
}

/** Which dialog is open; a document review names its document and decision. */
type Open =
  | 'verify'
  | 'requestInfo'
  | 'reject'
  | 'suspend'
  | 'reinstate'
  | 'kycVerify'
  | 'kycReject'
  | { doc: AdminBusinessDocument };

const KYC_ROWS: { field: AdminKycField; last4: (b: AdminBusiness) => string | null }[] = [
  { field: 'pan', last4: (b) => b.kyc.panLast4 },
  { field: 'gstin', last4: (b) => b.kyc.gstinLast4 },
  { field: 'upi', last4: (b) => b.kyc.upiLast4 },
  { field: 'bank', last4: (b) => b.kyc.bankLast4 },
];

function Detail({ b }: { b: AdminBusiness }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoles();
  const labels = useBusinessLabels();
  const storeLabels = useStoreLabels();
  const problemText = useProblemText();
  const move = useBusinessMove(b.id);
  const reveal = useReveal(b.id);
  const [open, setOpen] = useState<Open | null>(null);
  const [fields, setFields] = useState<AdminInfoField[]>([]);
  // Revealed values stay in this component only: never the query cache, the URL or storage.
  const [shown, setShown] = useState<Partial<Record<AdminKycField, string>>>({});
  const mayAct = can(roles, 'businessVerify');
  const mayReveal = can(roles, 'kycReveal');
  const now = b.serverNow;

  const { mutate, reset } = move;
  const close = useCallback(() => {
    setOpen(null);
    setFields([]);
    reset();
  }, [reset]);
  const run = useCallback(
    (m: BusinessMove, done: string) =>
      mutate(m, {
        onSuccess: () => {
          close();
          toast.show({ message: intl.formatMessage({ id: done }) });
        },
      }),
    [mutate, close, toast, intl],
  );

  const storeCols = useMemo<ColumnDef<AdminBusinessStore, unknown>[]>(
    () => [
      {
        id: 'name',
        header: intl.formatMessage({ id: 'businesses.col.store' }),
        cell: ({ row }) => (
          <Link to="/stores/$storeId" params={{ storeId: row.original.id }}>
            {row.original.name}
          </Link>
        ),
      },
      {
        id: 'place',
        header: intl.formatMessage({ id: 'businesses.col.place' }),
        accessorFn: (s) => [s.locality, s.city].filter(Boolean).join(', ') || '—',
      },
      {
        id: 'status',
        header: intl.formatMessage({ id: 'businesses.col.status' }),
        cell: ({ row }) => <StatusPill tone={storeTone(row.original.status)}>{storeLabels.status(row.original.status)}</StatusPill>,
      },
      { id: 'live', header: intl.formatMessage({ id: 'businesses.col.liveAt' }), accessorFn: (s) => (s.liveAt ? when(s.liveAt, now) : '—') },
    ],
    [intl, storeLabels, when, now],
  );

  const staffCols = useMemo<ColumnDef<AdminBusinessStaff, unknown>[]>(
    () => [
      { id: 'name', header: intl.formatMessage({ id: 'businesses.col.person' }), accessorFn: (p) => p.name ?? '—' },
      { id: 'role', header: intl.formatMessage({ id: 'businesses.col.role' }), accessorFn: (p) => labels.role(p.role) },
      { id: 'phone', header: intl.formatMessage({ id: 'businesses.leads.col.phone' }), accessorFn: (p) => p.phoneMasked ?? '—' },
      { id: 'status', header: intl.formatMessage({ id: 'businesses.col.status' }), accessorFn: (p) => labels.staffStatus(p.status) },
    ],
    [intl, labels],
  );

  const docCols = useMemo<ColumnDef<AdminBusinessDocument, unknown>[]>(
    () => [
      { id: 'kind', header: intl.formatMessage({ id: 'businesses.col.document' }), accessorFn: (d) => labels.docKind(d.kind) },
      { id: 'at', header: intl.formatMessage({ id: 'businesses.col.uploaded' }), accessorFn: (d) => when(d.createdAt, now) },
      {
        id: 'status',
        header: intl.formatMessage({ id: 'businesses.col.status' }),
        cell: ({ row }) => (
          <span className={css.stack}>
            <StatusPill tone={docTone(row.original.status)}>{labels.docStatus(row.original.status)}</StatusPill>
            {row.original.rejectReason && <span className={css.muted}>{row.original.rejectReason}</span>}
          </span>
        ),
      },
      ...(mayAct
        ? [
            {
              id: 'review',
              header: intl.formatMessage({ id: 'businesses.col.actions' }),
              cell: ({ row }: { row: { original: AdminBusinessDocument } }) => {
                const d = row.original;
                const kind = labels.docKind(d.kind);
                // A pending upload has nothing to review; the other decision corrects an earlier one.
                if (d.status === 'pending') return '—';
                return (
                  <span className={css.actions}>
                    {d.status !== 'accepted' && (
                      <Button
                        size="sm"
                        variant="soft"
                        aria-label={intl.formatMessage({ id: 'businesses.doc.acceptOne' }, { kind })}
                        loading={move.isPending && move.variables?.kind === 'document' && move.variables.docId === d.id}
                        onClick={() => run({ kind: 'document', docId: d.id, decision: 'accepted', reason: '' }, 'businesses.done.document')}
                      >
                        {intl.formatMessage({ id: 'businesses.doc.accept' })}
                      </Button>
                    )}
                    {d.status !== 'rejected' && (
                      <Button
                        size="sm"
                        variant="soft-danger"
                        aria-label={intl.formatMessage({ id: 'businesses.doc.rejectOne' }, { kind })}
                        onClick={() => setOpen({ doc: d })}
                      >
                        {intl.formatMessage({ id: 'businesses.doc.reject' })}
                      </Button>
                    )}
                  </span>
                );
              },
            } satisfies ColumnDef<AdminBusinessDocument, unknown>,
          ]
        : []),
    ],
    [intl, labels, when, now, mayAct, run, move.isPending, move.variables],
  );

  const error = move.isError ? problemText(move.error) : null;
  const doc = typeof open === 'object' && open !== null ? open : null;
  const kycOpen = b.kyc.status === 'submitted' || b.kyc.status === 'rejected';
  return (
    <div className={css.column}>
      <Link to="/businesses" className={css.muted}>
        {intl.formatMessage({ id: 'businesses.back' })}
      </Link>
      <div className={css.head}>
        <div>
          <div className={css.title}>
            <h1 className="tz-page-title">{b.brandName}</h1>
            <StatusPill tone={statusTone(b.status)}>{labels.status(b.status)}</StatusPill>
          </div>
          <p className="tz-page-sub">
            {intl.formatMessage({ id: 'businesses.detail.sub' }, { legal: b.legalName ?? '—', created: when(b.createdAt, now), stores: b.stores.length })}
          </p>
        </div>
        {mayAct && (
          <div className={css.actions}>
            {b.status === 'submitted' && (
              <>
                <Button variant="primary" icon={BadgeCheck} onClick={() => setOpen('verify')}>
                  {intl.formatMessage({ id: 'businesses.verify' })}
                </Button>
                <Button icon={MessageSquareWarning} onClick={() => setOpen('requestInfo')}>
                  {intl.formatMessage({ id: 'businesses.requestInfo' })}
                </Button>
              </>
            )}
            {(b.status === 'submitted' || b.status === 'needs_info') && (
              <Button variant="danger" icon={X} onClick={() => setOpen('reject')}>
                {intl.formatMessage({ id: 'businesses.reject' })}
              </Button>
            )}
            {b.status === 'verified' && (
              <Button variant="danger" icon={Ban} onClick={() => setOpen('suspend')}>
                {intl.formatMessage({ id: 'businesses.suspend' })}
              </Button>
            )}
            {b.status === 'suspended' && (
              <Button icon={RotateCcw} onClick={() => setOpen('reinstate')}>
                {intl.formatMessage({ id: 'businesses.reinstate' })}
              </Button>
            )}
          </div>
        )}
      </div>
      <div className={css.detail}>
        <div className={css.column}>
          <section className="tz-card" aria-labelledby="profile-title">
            <h2 id="profile-title" className="tz-card-title">
              {intl.formatMessage({ id: 'businesses.profile' })}
            </h2>
            <dl className={css.facts}>
              <dt>{intl.formatMessage({ id: 'businesses.col.legal' })}</dt>
              <dd>{b.legalName ?? '—'}</dd>
              <dt>{intl.formatMessage({ id: 'businesses.col.submitted' })}</dt>
              <dd>{b.submittedAt ? when(b.submittedAt, now) : '—'}</dd>
              <dt>{intl.formatMessage({ id: 'businesses.verifiedAt' })}</dt>
              <dd>{b.verifiedAt ? when(b.verifiedAt, now) : '—'}</dd>
              {b.status === 'needs_info' && (
                <>
                  <dt>{intl.formatMessage({ id: 'businesses.asked' })}</dt>
                  <dd>
                    {b.needsInfoFields.map((f) => labels.infoAny(f)).join(', ') || '—'}
                    {b.needsInfoNote && <span className={css.muted}> · {b.needsInfoNote}</span>}
                  </dd>
                </>
              )}
            </dl>
          </section>

          <section className="tz-card" aria-labelledby="kyc-title">
            <div className={css.head}>
              <div className={css.title}>
                <h2 id="kyc-title" className="tz-card-title">
                  {intl.formatMessage({ id: 'businesses.kyc' })}
                </h2>
                <StatusPill tone={kycTone(b.kyc.status)}>{labels.kyc(b.kyc.status)}</StatusPill>
              </div>
              {mayAct && kycOpen && (
                <div className={css.actions}>
                  <Button size="sm" variant="soft" onClick={() => setOpen('kycVerify')}>
                    {intl.formatMessage({ id: 'businesses.kyc.verify' })}
                  </Button>
                  {b.kyc.status === 'submitted' && (
                    <Button size="sm" variant="soft-danger" onClick={() => setOpen('kycReject')}>
                      {intl.formatMessage({ id: 'businesses.kyc.reject' })}
                    </Button>
                  )}
                </div>
              )}
            </div>
            <dl className={css.facts}>
              <dt>{intl.formatMessage({ id: 'businesses.kyc.legalName' })}</dt>
              <dd>{b.kyc.legalName ?? '—'}</dd>
              {KYC_ROWS.map(({ field, last4 }) => {
                const tail = last4(b);
                const value = shown[field];
                const label = labels.kycField(field);
                return (
                  <Fragment key={field}>
                    <dt>{label}</dt>
                    <dd className={css.title}>
                      <span className={css.mono}>{value ?? (tail ? intl.formatMessage({ id: 'businesses.kyc.masked' }, { last4: tail }) : '—')}</span>
                      {mayReveal && tail && !value && (
                        <Button
                          size="sm"
                          variant="ghost"
                          icon={Eye}
                          loading={reveal.isPending && reveal.variables === field}
                          aria-label={intl.formatMessage({ id: 'businesses.kyc.revealOne' }, { field: label })}
                          onClick={() =>
                            reveal.mutate(field, { onSuccess: (r) => setShown((s) => ({ ...s, [r.field]: r.value })) })
                          }
                        >
                          {intl.formatMessage({ id: 'businesses.kyc.reveal' })}
                        </Button>
                      )}
                      {value && (
                        <Button
                          size="sm"
                          variant="ghost"
                          icon={EyeOff}
                          aria-label={intl.formatMessage({ id: 'businesses.kyc.hideOne' }, { field: label })}
                          onClick={() => setShown(({ [field]: _gone, ...rest }) => rest)}
                        >
                          {intl.formatMessage({ id: 'businesses.kyc.hide' })}
                        </Button>
                      )}
                    </dd>
                  </Fragment>
                );
              })}
              <dt>{intl.formatMessage({ id: 'businesses.kyc.ifsc' })}</dt>
              <dd className={css.mono}>{b.kyc.bankIfsc ?? '—'}</dd>
              {b.kyc.notes && (
                <>
                  <dt>{intl.formatMessage({ id: 'businesses.kyc.notes' })}</dt>
                  <dd>{b.kyc.notes}</dd>
                </>
              )}
            </dl>
            {reveal.isError && (
              <p role="alert" className="tz-field-error">
                {problemText(reveal.error)}
              </p>
            )}
            {mayReveal && <p className={css.muted}>{intl.formatMessage({ id: 'businesses.kyc.revealNote' })}</p>}
          </section>

          <section className="tz-card" aria-labelledby="docs-title">
            <h2 id="docs-title" className="tz-card-title">
              {intl.formatMessage({ id: 'businesses.documents' })}
            </h2>
            {open === null && move.isError && move.variables?.kind === 'document' && (
              <p role="alert" className="tz-field-error">
                {problemText(move.error)}
              </p>
            )}
            <DataTable
              label={intl.formatMessage({ id: 'businesses.documents' })}
              columns={docCols}
              data={b.documents}
              getRowId={(d) => d.id}
              emptyLabel={intl.formatMessage({ id: 'businesses.documents.none' })}
            />
          </section>

          <section className="tz-card" aria-labelledby="stores-title">
            <h2 id="stores-title" className="tz-card-title">
              {intl.formatMessage({ id: 'businesses.stores' })}
            </h2>
            <DataTable
              label={intl.formatMessage({ id: 'businesses.stores' })}
              columns={storeCols}
              data={b.stores}
              getRowId={(s) => s.id}
              emptyLabel={intl.formatMessage({ id: 'businesses.stores.none' })}
            />
          </section>

          <section className="tz-card" aria-labelledby="staff-title">
            <h2 id="staff-title" className="tz-card-title">
              {intl.formatMessage({ id: 'businesses.staff' })}
            </h2>
            <DataTable
              label={intl.formatMessage({ id: 'businesses.staff' })}
              columns={staffCols}
              data={b.staff}
              getRowId={(p) => p.id}
              emptyLabel={intl.formatMessage({ id: 'businesses.staff.none' })}
            />
          </section>
        </div>
        <AuditPanel type="business" id={b.id} />
      </div>

      <ReasonDialog
        open={open === 'verify'}
        title={intl.formatMessage({ id: 'businesses.verify.title' }, { name: b.brandName })}
        description={intl.formatMessage({ id: 'businesses.verify.body' })}
        confirmLabel={intl.formatMessage({ id: 'businesses.verify' })}
        label={intl.formatMessage({ id: 'businesses.note.label' })}
        help={intl.formatMessage({ id: 'businesses.note.optional' })}
        optional
        pending={move.isPending}
        error={error}
        onConfirm={(note) => run({ kind: 'verify', note }, 'businesses.done.verify')}
        onClose={close}
      />
      <ReasonDialog
        open={open === 'requestInfo'}
        title={intl.formatMessage({ id: 'businesses.requestInfo.title' }, { name: b.brandName })}
        description={intl.formatMessage({ id: 'businesses.requestInfo.body' })}
        confirmLabel={intl.formatMessage({ id: 'businesses.requestInfo' })}
        label={intl.formatMessage({ id: 'businesses.requestInfo.note' })}
        help={intl.formatMessage({ id: 'businesses.requestInfo.noteHelp' })}
        blocked={fields.length === 0}
        pending={move.isPending}
        error={error}
        onConfirm={(note) => run({ kind: 'requestInfo', fields, note }, 'businesses.done.requestInfo')}
        onClose={close}
      >
        <fieldset className="tz-field">
          <legend className="tz-field-label">{intl.formatMessage({ id: 'businesses.requestInfo.fields' })}</legend>
          <div className={css.filters}>
            {INFO_FIELDS.map((f) => (
              <label key={f} className={css.title}>
                <input
                  type="checkbox"
                  checked={fields.includes(f)}
                  onChange={(e) => setFields((cur) => (e.target.checked ? [...cur, f] : cur.filter((x) => x !== f)))}
                />
                {labels.info(f)}
              </label>
            ))}
          </div>
          {fields.length === 0 && <p className="tz-field-help">{intl.formatMessage({ id: 'businesses.requestInfo.pick' })}</p>}
        </fieldset>
      </ReasonDialog>
      <ReasonDialog
        open={open === 'reject'}
        title={intl.formatMessage({ id: 'businesses.reject.title' }, { name: b.brandName })}
        description={intl.formatMessage({ id: 'businesses.reject.body' })}
        confirmLabel={intl.formatMessage({ id: 'businesses.reject' })}
        danger
        pending={move.isPending}
        error={error}
        onConfirm={(reason) => run({ kind: 'reject', reason }, 'businesses.done.reject')}
        onClose={close}
      />
      <ReasonDialog
        open={open === 'suspend'}
        title={intl.formatMessage({ id: 'businesses.suspend.title' }, { name: b.brandName })}
        description={intl.formatMessage({ id: 'businesses.suspend.body' })}
        confirmLabel={intl.formatMessage({ id: 'businesses.suspend' })}
        danger
        pending={move.isPending}
        error={error}
        onConfirm={(reason) => run({ kind: 'suspend', reason }, 'businesses.done.suspend')}
        onClose={close}
      />
      <ReasonDialog
        open={open === 'reinstate'}
        title={intl.formatMessage({ id: 'businesses.reinstate.title' }, { name: b.brandName })}
        description={intl.formatMessage({ id: 'businesses.reinstate.body' })}
        confirmLabel={intl.formatMessage({ id: 'businesses.reinstate' })}
        pending={move.isPending}
        error={error}
        onConfirm={(reason) => run({ kind: 'reinstate', reason }, 'businesses.done.reinstate')}
        onClose={close}
      />
      <ReasonDialog
        open={open === 'kycVerify'}
        title={intl.formatMessage({ id: 'businesses.kyc.verify.title' })}
        description={intl.formatMessage({ id: 'businesses.kyc.verify.body' })}
        confirmLabel={intl.formatMessage({ id: 'businesses.kyc.verify' })}
        label={intl.formatMessage({ id: 'businesses.note.label' })}
        help={intl.formatMessage({ id: 'businesses.note.optional' })}
        optional
        pending={move.isPending}
        error={error}
        onConfirm={(note) => run({ kind: 'kyc', decision: 'verified', note }, 'businesses.done.kyc')}
        onClose={close}
      />
      <ReasonDialog
        open={open === 'kycReject'}
        title={intl.formatMessage({ id: 'businesses.kyc.reject.title' })}
        description={intl.formatMessage({ id: 'businesses.kyc.reject.body' })}
        confirmLabel={intl.formatMessage({ id: 'businesses.kyc.reject' })}
        danger
        pending={move.isPending}
        error={error}
        onConfirm={(note) => run({ kind: 'kyc', decision: 'rejected', note }, 'businesses.done.kyc')}
        onClose={close}
      />
      <ReasonDialog
        open={doc !== null}
        title={doc ? intl.formatMessage({ id: 'businesses.doc.reject.title' }, { kind: labels.docKind(doc.doc.kind) }) : ''}
        description={intl.formatMessage({ id: 'businesses.doc.reject.body' })}
        confirmLabel={intl.formatMessage({ id: 'businesses.doc.reject' })}
        danger
        pending={move.isPending}
        error={error}
        onConfirm={(reason) => doc && run({ kind: 'document', docId: doc.doc.id, decision: 'rejected', reason }, 'businesses.done.document')}
        onClose={close}
      />
    </div>
  );
}
