import type { AdminRole } from '@todayz/api-client/gen/model/adminRole';
import type { AdminUser } from '@todayz/api-client/gen/model/adminUser';
import type { AdminUserStatus } from '@todayz/api-client/gen/model/adminUserStatus';
import { ApiError } from '@todayz/api-client';
import { Button, DataTable, StatusPill, Tabs, TextField, useToast, type ColumnDef } from '@todayz/ui';
import { useNavigate } from '@tanstack/react-router';
import { LockOpen, RotateCcw, ShieldCheck, UserMinus, UserPlus } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';
import { useIntl } from 'react-intl';
import { useWhen } from '../app/format';
import { useMe } from '../app/nav/access';
import { useProblemText } from '../app/problems';
import { ReasonDialog } from '../app/ReasonDialog';
import { StepUpDialog } from '../app/StepUpDialog';
import { AuditPanel } from '../audit/AuditPanel';
import { ADMIN_ROLES, phoneOk, useAccountError, useRoleLabel } from './labels';
import type { AdminUserChange } from '@todayz/api-client/gen/model/adminUserChange';
import { useAccountMove, useAdminUsers, useInvite, useUnlockSignIn } from './queries';
import type { AdminUsersSearch } from './search';
import css from '../app/page.module.css';

type Go = (next: AdminUsersSearch) => void;

/** Keeps the change a `403 step_up_required` stopped, and sends it again once the code is right. */
function useStepUp() {
  const [retry, setRetry] = useState<(() => void) | null>(null);
  const ask = useCallback((err: unknown, again: () => void) => {
    if (err instanceof ApiError && err.code === 'step_up_required') setRetry(() => again);
  }, []);
  const dialog = (
    <StepUpDialog
      open={retry !== null}
      onVerified={() => {
        const again = retry;
        setRetry(null);
        again?.();
      }}
      onClose={() => setRetry(null)}
    />
  );
  return { ask, done: () => setRetry(null), dialog };
}

/** A-18 Admin users (super_admin): who has admin access, with which roles; invite, change roles, disable, enable. */
export function AdminUsersPage({ search }: { search: AdminUsersSearch }) {
  const intl = useIntl();
  const navigate = useNavigate();
  const go: Go = useCallback((next) => void navigate({ to: '/admin-users', search: next }), [navigate]);
  const [inviting, setInviting] = useState(false);
  const status: AdminUserStatus = search.status ?? 'active';
  return (
    <div className={css.column}>
      <div className={css.head}>
        <div>
          <h1 className="tz-page-title">{intl.formatMessage({ id: 'shell.nav.adminUsers' })}</h1>
          <p className="tz-page-sub">{intl.formatMessage({ id: 'adminUsers.sub' })}</p>
        </div>
        <Button variant="primary" icon={UserPlus} onClick={() => setInviting(true)}>
          {intl.formatMessage({ id: 'adminUsers.invite' })}
        </Button>
      </div>
      <Tabs
        label={intl.formatMessage({ id: 'adminUsers.tabs' })}
        tabs={[
          { value: 'active', label: intl.formatMessage({ id: 'adminUsers.tab.active' }) },
          { value: 'disabled', label: intl.formatMessage({ id: 'adminUsers.tab.disabled' }) },
        ]}
        value={status}
        onChange={(v) => go(v === 'disabled' ? { status: 'disabled' } : {})}
      />
      <AccountList status={status} search={search} go={go} />
      {inviting && <InviteDialog onClose={() => setInviting(false)} onDone={(id) => go({ admin: id })} />}
    </div>
  );
}

function AccountList({ status, search, go }: { status: AdminUserStatus; search: AdminUsersSearch; go: Go }) {
  const intl = useIntl();
  const when = useWhen();
  const roles = useRoleLabel();
  const problemText = useProblemText();
  const q = useAdminUsers(status);
  const pages = q.data?.pages ?? [];
  const rows = pages.flatMap((p) => p.items);
  const now = pages[0]?.serverNow;
  const selected = search.admin ? rows.find((a) => a.id === search.admin) : undefined;

  const columns = useMemo<ColumnDef<AdminUser, unknown>[]>(
    () => [
      {
        id: 'person',
        header: intl.formatMessage({ id: 'adminUsers.col.person' }),
        cell: ({ row }) => (
          <div>
            <Button size="sm" variant="ghost" aria-pressed={row.original.id === search.admin} onClick={() => go({ ...search, admin: row.original.id })}>
              {row.original.name ?? intl.formatMessage({ id: 'adminUsers.noName' })}
            </Button>
            {row.original.phoneMasked && <div className={`${css.muted} ${css.mono}`}>{row.original.phoneMasked}</div>}
          </div>
        ),
      },
      { id: 'roles', header: intl.formatMessage({ id: 'adminUsers.col.roles' }), accessorFn: (a) => roles.list(a.roles) },
      {
        id: 'active',
        header: intl.formatMessage({ id: status === 'disabled' ? 'adminUsers.col.disabled' : 'adminUsers.col.lastActive' }),
        accessorFn: (a) => {
          const at = status === 'disabled' ? a.disabledAt : a.lastActiveAt;
          return at ? when(at, now ?? at) : intl.formatMessage({ id: 'adminUsers.never' });
        },
      },
    ],
    [intl, roles, when, now, status, search, go],
  );

  return (
    <div className={css.detail}>
      <div className={css.column}>
        {q.isError ? (
          <p role="alert" className="tz-field-error">
            {problemText(q.error)}{' '}
            <Button size="sm" variant="ghost" onClick={() => void q.refetch()}>
              {intl.formatMessage({ id: 'common.retry' })}
            </Button>
          </p>
        ) : (
          <DataTable
            label={intl.formatMessage({ id: status === 'disabled' ? 'adminUsers.tab.disabled' : 'adminUsers.tab.active' })}
            columns={columns}
            data={rows}
            getRowId={(a) => a.id}
            loading={q.isPending}
            emptyLabel={intl.formatMessage({ id: status === 'disabled' ? 'adminUsers.empty.disabled' : 'adminUsers.empty' })}
            footer={
              q.hasNextPage ? (
                <Button size="sm" variant="soft" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
                  {intl.formatMessage({ id: 'common.loadMore' })}
                </Button>
              ) : undefined
            }
          />
        )}
      </div>
      <div className={css.column}>
        {selected ? (
          <AccountCard key={selected.id} a={selected} now={now ?? selected.createdAt} />
        ) : (
          <p className={`tz-card ${css.muted}`}>
            {intl.formatMessage({ id: search.admin && !q.isPending ? 'adminUsers.pick.notListed' : 'adminUsers.pick' })}
          </p>
        )}
      </div>
    </div>
  );
}

/** Role tick boxes; at least one stays ticked before the dialog sends. */
function RolePicker({ value, onChange }: { value: readonly AdminRole[]; onChange: (next: AdminRole[]) => void }) {
  const intl = useIntl();
  const roles = useRoleLabel();
  return (
    <fieldset className="tz-field">
      <legend className="tz-field-label">{intl.formatMessage({ id: 'adminUsers.roles' })}</legend>
      <div className={css.filters}>
        {ADMIN_ROLES.map((r) => (
          <label key={r} className={css.title}>
            <input
              type="checkbox"
              checked={value.includes(r)}
              onChange={(e) => onChange(e.target.checked ? ADMIN_ROLES.filter((x) => x === r || value.includes(x)) : value.filter((x) => x !== r))}
            />
            {roles.one(r)}
          </label>
        ))}
      </div>
      <p className={value.length === 0 ? 'tz-field-error' : 'tz-field-help'}>
        {intl.formatMessage({ id: value.length === 0 ? 'adminUsers.roles.pick' : 'adminUsers.roles.help' })}
      </p>
    </fieldset>
  );
}

/** Give a phone number admin access. The number stays in this dialog's state only and goes out in the POST body. */
function InviteDialog({ onClose, onDone }: { onClose: () => void; onDone: (id: string) => void }) {
  const intl = useIntl();
  const toast = useToast();
  const invite = useInvite();
  const errorText = useAccountError();
  const step = useStepUp();
  const [phone, setPhone] = useState('');
  const [name, setName] = useState('');
  const [picked, setPicked] = useState<AdminRole[]>([]);
  const phoneBad = phone.trim() !== '' && !phoneOk(phone);
  const ready = phoneOk(phone) && name.trim() !== '' && picked.length > 0;

  const send = (reason: string) =>
    invite.mutate(
      { phone: phone.trim(), name: name.trim(), roles: picked, reason },
      {
        onSuccess: (a) => {
          step.done();
          toast.show({ message: intl.formatMessage({ id: 'adminUsers.done.invite' }, { name: a.name ?? '' }) });
          onDone(a.id);
          onClose();
        },
        onError: (e) => step.ask(e, () => send(reason)),
      },
    );

  return (
    <>
      <ReasonDialog
        open
        title={intl.formatMessage({ id: 'adminUsers.invite.title' })}
        description={intl.formatMessage({ id: 'adminUsers.invite.body' })}
        confirmLabel={intl.formatMessage({ id: 'adminUsers.invite' })}
        blocked={!ready}
        pending={invite.isPending}
        error={errorText(invite.error, 'invite')}
        onConfirm={send}
        onClose={onClose}
      >
        <TextField
          label={intl.formatMessage({ id: 'adminUsers.invite.phone' })}
          type="tel"
          inputMode="tel"
          autoComplete="off"
          maxLength={16}
          className="tz-num"
          value={phone}
          onChange={(e) => setPhone(e.target.value)}
          help={intl.formatMessage({ id: 'adminUsers.invite.phoneHelp' })}
          error={phoneBad ? intl.formatMessage({ id: 'adminUsers.invite.phoneBad' }) : undefined}
        />
        <TextField
          label={intl.formatMessage({ id: 'adminUsers.invite.name' })}
          autoComplete="off"
          maxLength={80}
          value={name}
          onChange={(e) => setName(e.target.value)}
          help={intl.formatMessage({ id: 'adminUsers.invite.nameHelp' })}
        />
        <RolePicker value={picked} onChange={setPicked} />
      </ReasonDialog>
      {step.dialog}
    </>
  );
}

type Asking = 'roles' | 'disable' | 'enable' | 'unlock' | null;

/**
 * The chosen account: its facts, the moves on it (never on your own), and its history. While its
 * sign-in is locked after too many wrong codes (D-264), "Unlock sign-in" lifts the lock.
 */
function AccountCard({ a, now }: { a: AdminUser; now: string }) {
  const intl = useIntl();
  const when = useWhen();
  const toast = useToast();
  const roles = useRoleLabel();
  const errorText = useAccountError();
  const me = useMe().data;
  const move = useAccountMove(a.id);
  const unlock = useUnlockSignIn(a.id);
  const step = useStepUp();
  const [asking, setAsking] = useState<Asking>(null);
  const [picked, setPicked] = useState<AdminRole[]>(a.roles);
  const self = me?.id === a.id;
  const name = a.name ?? intl.formatMessage({ id: 'adminUsers.noName' });
  const sameRoles = picked.length === a.roles.length && picked.every((r) => a.roles.includes(r));

  const open = (k: Exclude<Asking, null>) => {
    move.reset();
    unlock.reset();
    setPicked(a.roles);
    setAsking(k);
  };
  const run = (body: AdminUserChange, done: string) =>
    move.mutate(body, {
      onSuccess: () => {
        step.done();
        setAsking(null);
        toast.show({ message: intl.formatMessage({ id: done }, { name }) });
      },
      onError: (e) => step.ask(e, () => run(body, done)),
    });
  const change = (body: Omit<AdminUserChange, 'version'>, done: string) => run({ ...body, version: a.version }, done);
  const error = errorText(move.error, 'change');
  const lifted = (reason: string) =>
    unlock.mutate(
      { reason },
      {
        onSuccess: () => {
          step.done();
          setAsking(null);
          toast.show({ message: intl.formatMessage({ id: 'adminUsers.done.unlock' }, { name }) });
        },
        onError: (e) => step.ask(e, () => lifted(reason)),
      },
    );
  const locked = a.signInLockedUntil ?? null;

  return (
    <>
      <section className="tz-card" aria-labelledby={`admin-${a.id}`}>
        <h2 id={`admin-${a.id}`} className="tz-card-title">
          {name}
        </h2>
        <dl className={css.facts}>
          <dt>{intl.formatMessage({ id: 'adminUsers.detail.phone' })}</dt>
          <dd className={css.mono}>{a.phoneMasked ?? '—'}</dd>
          <dt>{intl.formatMessage({ id: 'adminUsers.col.roles' })}</dt>
          <dd>{roles.list(a.roles)}</dd>
          <dt>{intl.formatMessage({ id: 'adminUsers.col.status' })}</dt>
          <dd>
            <StatusPill tone={a.status === 'active' ? 'completed' : 'cancelled'}>
              {intl.formatMessage({ id: a.status === 'active' ? 'adminUsers.status.active' : 'adminUsers.status.disabled' })}
            </StatusPill>
          </dd>
          <dt>{intl.formatMessage({ id: 'adminUsers.col.lastActive' })}</dt>
          <dd>{a.lastActiveAt ? when(a.lastActiveAt, now) : intl.formatMessage({ id: 'adminUsers.never' })}</dd>
          <dt>{intl.formatMessage({ id: 'adminUsers.detail.invited' })}</dt>
          <dd>
            {a.createdBy
              ? intl.formatMessage({ id: 'adminUsers.detail.invitedBy' }, { name: a.createdByName ?? '—', when: when(a.createdAt, now) })
              : intl.formatMessage({ id: 'adminUsers.detail.seeded' }, { when: when(a.createdAt, now) })}
          </dd>
          {a.disabledAt && (
            <>
              <dt>{intl.formatMessage({ id: 'adminUsers.col.disabled' })}</dt>
              <dd>{when(a.disabledAt, now)}</dd>
            </>
          )}
          {locked && (
            <>
              <dt>{intl.formatMessage({ id: 'adminUsers.detail.signIn' })}</dt>
              <dd>
                <StatusPill tone="cancelled">{intl.formatMessage({ id: 'adminUsers.signInLocked' })}</StatusPill>{' '}
                {intl.formatMessage({ id: 'adminUsers.signInLockedUntil' }, { when: when(locked, now) })}
              </dd>
            </>
          )}
        </dl>
        {self ? (
          <p className={css.muted}>{intl.formatMessage({ id: 'adminUsers.self' })}</p>
        ) : (
          <div className={css.actions}>
            {locked && (
              <Button variant="primary" icon={LockOpen} onClick={() => open('unlock')}>
                {intl.formatMessage({ id: 'adminUsers.unlock' })}
              </Button>
            )}
            {a.status === 'active' && (
              <Button variant="soft" icon={ShieldCheck} onClick={() => open('roles')}>
                {intl.formatMessage({ id: 'adminUsers.changeRoles' })}
              </Button>
            )}
            {a.status === 'active' ? (
              <Button variant="danger" icon={UserMinus} onClick={() => open('disable')}>
                {intl.formatMessage({ id: 'adminUsers.disable' })}
              </Button>
            ) : (
              <Button variant="primary" icon={RotateCcw} onClick={() => open('enable')}>
                {intl.formatMessage({ id: 'adminUsers.enable' })}
              </Button>
            )}
          </div>
        )}
      </section>
      <AuditPanel type="admin_user" id={a.id} />
      <ReasonDialog
        open={asking === 'roles'}
        title={intl.formatMessage({ id: 'adminUsers.roles.title' }, { name })}
        description={intl.formatMessage({ id: 'adminUsers.roles.body' })}
        confirmLabel={intl.formatMessage({ id: 'adminUsers.changeRoles' })}
        blocked={picked.length === 0 || sameRoles}
        pending={move.isPending}
        error={error}
        onConfirm={(reason) => change({ roles: picked, reason }, 'adminUsers.done.roles')}
        onClose={() => setAsking(null)}
      >
        <RolePicker value={picked} onChange={setPicked} />
        {sameRoles && <p className="tz-field-help">{intl.formatMessage({ id: 'adminUsers.roles.same' })}</p>}
      </ReasonDialog>
      <ReasonDialog
        open={asking === 'disable'}
        title={intl.formatMessage({ id: 'adminUsers.disable.title' }, { name })}
        description={intl.formatMessage({ id: 'adminUsers.disable.body' })}
        confirmLabel={intl.formatMessage({ id: 'adminUsers.disable' })}
        danger
        pending={move.isPending}
        error={error}
        onConfirm={(reason) => change({ status: 'disabled', reason }, 'adminUsers.done.disable')}
        onClose={() => setAsking(null)}
      />
      <ReasonDialog
        open={asking === 'enable'}
        title={intl.formatMessage({ id: 'adminUsers.enable.title' }, { name })}
        description={intl.formatMessage({ id: 'adminUsers.enable.body' })}
        confirmLabel={intl.formatMessage({ id: 'adminUsers.enable' })}
        pending={move.isPending}
        error={error}
        onConfirm={(reason) => change({ status: 'active', reason }, 'adminUsers.done.enable')}
        onClose={() => setAsking(null)}
      />
      <ReasonDialog
        open={asking === 'unlock'}
        title={intl.formatMessage({ id: 'adminUsers.unlock.title' }, { name })}
        description={intl.formatMessage({ id: 'adminUsers.unlock.body' })}
        confirmLabel={intl.formatMessage({ id: 'adminUsers.unlock' })}
        pending={unlock.isPending}
        error={errorText(unlock.error, 'unlock')}
        onConfirm={lifted}
        onClose={() => setAsking(null)}
      />
      {step.dialog}
    </>
  );
}
