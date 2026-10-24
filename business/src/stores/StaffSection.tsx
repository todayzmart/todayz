import { inviteStaff, removeStaff, updateStaff } from '@todayz/api-client/gen/business/business';
import type { StaffInviteRole } from '@todayz/api-client/gen/model/staffInviteRole';
import type { StaffMember } from '@todayz/api-client/gen/model/staffMember';
import type { Store } from '@todayz/api-client/gen/model/store';
import { Button, Dialog, Sheet, Skeleton, StatusPill, TextField, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';
import { useState } from 'react';
import { useIntl } from 'react-intl';
import type { BusinessSummary } from '../app/shop';
import { BizError } from './BizError';
import { canEditStaff, canRemoveStaff, canSeeStaff, inviteRoles } from './perm';
import { staffKey, useAction, useStaff } from './queries';
import styles from './stores.module.css';

const MOBILE = /^[6-9][0-9]{9}$/;

/** B-13 staff: who works here, and (by role) invite, change and remove (D-66). */
export function StaffSection({ business, stores }: { business: BusinessSummary; stores: Store[] }) {
  const intl = useIntl();
  const role = business.role;
  const bid = business.id;
  const staff = useStaff(bid, canSeeStaff(role));
  const [inviting, setInviting] = useState(false);
  const [editing, setEditing] = useState<StaffMember | null>(null);
  const [removing, setRemoving] = useState<StaffMember | null>(null);
  if (!canSeeStaff(role)) return null;
  const items = staff.data?.items ?? [];
  return (
    <section className="tz-card" aria-labelledby="staff-h">
      <div className={styles.head}>
        <h2 id="staff-h" className="tz-card-title">
          {intl.formatMessage({ id: 'stores.section.staff' })}
        </h2>
        {inviteRoles(role).length > 0 && (
          <Button icon={Plus} onClick={() => setInviting(true)}>
            {intl.formatMessage({ id: 'stores.staff.invite' })}
          </Button>
        )}
      </div>
      {staff.isLoading && <Skeleton height="4rem" />}
      {staff.isError && <BizError error={staff.error} />}
      {staff.data && items.length <= 1 && <p className="tz-card-desc">{intl.formatMessage({ id: 'stores.staff.empty' })}</p>}
      <ul className={styles.list}>
        {items.map((m) => (
          <StaffRow key={m.id} member={m} role={role} onEdit={() => setEditing(m)} onRemove={() => setRemoving(m)} />
        ))}
      </ul>
      {inviting && <InviteSheet bid={bid} role={role} stores={stores} onClose={() => setInviting(false)} />}
      {editing && <EditStaffSheet bid={bid} member={editing} stores={stores} onClose={() => setEditing(null)} />}
      {removing && <RemoveDialog bid={bid} member={removing} onClose={() => setRemoving(null)} />}
    </section>
  );
}

const displayName = (m: StaffMember, fallback: string) => m.name ?? m.phoneMasked ?? fallback;

function StaffRow({ member, role, onEdit, onRemove }: { member: StaffMember; role: BusinessSummary['role']; onEdit: () => void; onRemove: () => void }) {
  const intl = useIntl();
  const name = displayName(member, intl.formatMessage({ id: 'stores.staff.unnamed' }));
  const scope =
    member.storeIds === null
      ? intl.formatMessage({ id: 'stores.staff.allStores' })
      : intl.formatMessage({ id: 'stores.staff.someStores' }, { count: member.storeIds.length });
  const expires = member.inviteExpiresAt
    ? intl.formatDate(member.inviteExpiresAt, { day: 'numeric', month: 'short', timeZone: 'Asia/Kolkata' })
    : null;
  return (
    <li className={styles.row}>
      <div className={styles.rowMain}>
        <span className={styles.rowName}>{name}</span>
        <span className={styles.meta}>
          {[
            intl.formatMessage({ id: `stores.staff.role.${member.role}` }),
            member.status === 'invited' && expires ? intl.formatMessage({ id: 'stores.staff.expires' }, { date: expires }) : null,
            member.role === 'owner' ? null : scope,
            member.canDeliver ? intl.formatMessage({ id: 'stores.staff.delivers' }) : null,
          ]
            .filter(Boolean)
            .join(' · ')}
        </span>
      </div>
      <div className={styles.pills}>
        <StatusPill tone={member.status === 'active' ? 'ready' : 'pending'}>{intl.formatMessage({ id: `stores.staff.status.${member.status}` })}</StatusPill>
        {canEditStaff(role, member.role) && (
          <Button aria-label={intl.formatMessage({ id: 'stores.staff.editNamed' }, { name })} onClick={onEdit}>
            {intl.formatMessage({ id: 'stores.staff.edit' })}
          </Button>
        )}
        {canRemoveStaff(role, member.role) && (
          <Button variant="danger" aria-label={intl.formatMessage({ id: 'stores.staff.removeNamed' }, { name })} onClick={onRemove}>
            {intl.formatMessage({ id: 'stores.staff.remove' })}
          </Button>
        )}
      </div>
    </li>
  );
}

interface ScopeValue {
  all: boolean;
  ids: string[];
}

function ScopePicker({ value, stores, onChange, error }: { value: ScopeValue; stores: Store[]; onChange: (v: ScopeValue) => void; error: boolean }) {
  const intl = useIntl();
  return (
    <fieldset className={styles.fieldset}>
      <legend className={styles.legend}>{intl.formatMessage({ id: 'stores.staff.scope' })}</legend>
      <label className={styles.check}>
        <input type="radio" name="scope" checked={value.all} onChange={() => onChange({ ...value, all: true })} />
        <span>{intl.formatMessage({ id: 'stores.staff.scopeAll' })}</span>
      </label>
      <label className={styles.check}>
        <input type="radio" name="scope" checked={!value.all} onChange={() => onChange({ ...value, all: false })} />
        <span>{intl.formatMessage({ id: 'stores.staff.scopeSome' })}</span>
      </label>
      {!value.all &&
        stores.map((s) => (
          <label key={s.id} className={styles.check}>
            <input
              type="checkbox"
              checked={value.ids.includes(s.id)}
              onChange={(e) => onChange({ ...value, ids: e.target.checked ? [...value.ids, s.id] : value.ids.filter((x) => x !== s.id) })}
            />
            <span>{s.name}</span>
          </label>
        ))}
      {error && (
        <p role="alert" className="tz-field-error">
          {intl.formatMessage({ id: 'stores.staff.scopeNone' })}
        </p>
      )}
    </fieldset>
  );
}

function RolePicker({ roles, value, onChange }: { roles: readonly StaffInviteRole[]; value: StaffInviteRole; onChange: (r: StaffInviteRole) => void }) {
  const intl = useIntl();
  return (
    <fieldset className={styles.fieldset}>
      <legend className={styles.legend}>{intl.formatMessage({ id: 'stores.staff.role' })}</legend>
      {roles.map((r) => (
        <label key={r} className={styles.check}>
          <input type="radio" name="staff-role" checked={value === r} onChange={() => onChange(r)} />
          <span>
            {intl.formatMessage({ id: `stores.staff.role.${r}` })}
            <span className={styles.note}> {intl.formatMessage({ id: `stores.staff.roleHelp.${r}` })}</span>
          </span>
        </label>
      ))}
    </fieldset>
  );
}

function InviteSheet({ bid, role, stores, onClose }: { bid: string; role: BusinessSummary['role']; stores: Store[]; onClose: () => void }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const action = useAction();
  const roles = inviteRoles(role);
  const [phone, setPhone] = useState('');
  const [pick, setPick] = useState<StaffInviteRole>(roles[0] ?? 'cashier');
  const [scope, setScope] = useState<ScopeValue>({ all: true, ids: [] });
  const [deliver, setDeliver] = useState(false);
  const [shown, setShown] = useState(false);
  const phoneBad = !MOBILE.test(phone.trim());
  const scopeBad = !scope.all && scope.ids.length === 0;
  const send = async () => {
    setShown(true);
    if (phoneBad || scopeBad) return;
    const out = await action.run('invite', (key) =>
      inviteStaff(bid, { phone: `+91${phone.trim()}`, role: pick, storeIds: scope.all ? null : scope.ids, canDeliver: deliver }, { idempotencyKey: key }),
    );
    if (!out) return;
    await qc.invalidateQueries({ queryKey: staffKey(bid) });
    toast.show({ message: intl.formatMessage({ id: 'stores.staff.invited' }) });
    onClose();
  };
  return (
    <Sheet
      open
      onClose={onClose}
      title={intl.formatMessage({ id: 'stores.staff.inviteTitle' })}
      description={intl.formatMessage({ id: 'stores.staff.inviteBody' })}
      closeLabel={intl.formatMessage({ id: 'stores.sheet.close' })}
    >
      <form
        className={styles.stack}
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          void send();
        }}
      >
        <TextField
          label={intl.formatMessage({ id: 'stores.staff.phone' })}
          help={intl.formatMessage({ id: 'stores.staff.phoneHelp' })}
          prefix="+91"
          value={phone}
          inputMode="tel"
          maxLength={10}
          autoComplete="off"
          onChange={(e) => setPhone(e.target.value)}
          error={shown && phoneBad ? intl.formatMessage({ id: 'stores.form.err.phone' }) : undefined}
        />
        <RolePicker roles={roles} value={pick} onChange={setPick} />
        <ScopePicker value={scope} stores={stores} onChange={setScope} error={shown && scopeBad} />
        <label className={styles.check}>
          <input type="checkbox" checked={deliver} onChange={(e) => setDeliver(e.target.checked)} />
          <span>{intl.formatMessage({ id: 'stores.staff.canDeliver' })}</span>
        </label>
        <BizError error={action.error} />
        <div className={styles.actions}>
          <Button type="submit" variant="primary" loading={action.pending}>
            {intl.formatMessage({ id: 'stores.staff.sendInvite' })}
          </Button>
          <Button onClick={onClose}>{intl.formatMessage({ id: 'stores.cancel' })}</Button>
        </div>
      </form>
    </Sheet>
  );
}

function EditStaffSheet({ bid, member, stores, onClose }: { bid: string; member: StaffMember; stores: Store[]; onClose: () => void }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const action = useAction();
  const name = displayName(member, intl.formatMessage({ id: 'stores.staff.unnamed' }));
  const [pick, setPick] = useState<StaffInviteRole>(member.role === 'manager' ? 'manager' : 'cashier');
  const [scope, setScope] = useState<ScopeValue>({ all: member.storeIds === null, ids: member.storeIds ?? [] });
  const [deliver, setDeliver] = useState(member.canDeliver);
  const scopeBad = !scope.all && scope.ids.length === 0;
  const [shown, setShown] = useState(false);
  const save = async () => {
    setShown(true);
    if (scopeBad) return;
    const out = await action.run(`staff-${member.id}`, () =>
      updateStaff(bid, member.id, { role: pick, storeIds: scope.all ? null : scope.ids, canDeliver: deliver }),
    );
    if (!out) return;
    await qc.invalidateQueries({ queryKey: staffKey(bid) });
    toast.show({ message: intl.formatMessage({ id: 'stores.saved' }) });
    onClose();
  };
  return (
    <Sheet
      open
      onClose={onClose}
      title={intl.formatMessage({ id: 'stores.staff.editTitle' }, { name })}
      description={intl.formatMessage({ id: 'stores.staff.editBody' })}
      closeLabel={intl.formatMessage({ id: 'stores.sheet.close' })}
    >
      <form
        className={styles.stack}
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <RolePicker roles={['manager', 'cashier']} value={pick} onChange={setPick} />
        <ScopePicker value={scope} stores={stores} onChange={setScope} error={shown && scopeBad} />
        <label className={styles.check}>
          <input type="checkbox" checked={deliver} onChange={(e) => setDeliver(e.target.checked)} />
          <span>{intl.formatMessage({ id: 'stores.staff.canDeliver' })}</span>
        </label>
        <BizError error={action.error} />
        <div className={styles.actions}>
          <Button type="submit" variant="primary" loading={action.pending}>
            {intl.formatMessage({ id: 'stores.staff.saveChanges' })}
          </Button>
          <Button onClick={onClose}>{intl.formatMessage({ id: 'stores.cancel' })}</Button>
        </div>
      </form>
    </Sheet>
  );
}

/** Removing someone ends their access at once, so it asks first (D-66). */
function RemoveDialog({ bid, member, onClose }: { bid: string; member: StaffMember; onClose: () => void }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const action = useAction();
  const name = displayName(member, intl.formatMessage({ id: 'stores.staff.unnamed' }));
  const confirm = async () => {
    const out = await action.run(`remove-${member.id}`, async () => {
      await removeStaff(bid, member.id);
      return true;
    });
    if (!out) return;
    await qc.invalidateQueries({ queryKey: staffKey(bid) });
    toast.show({ message: intl.formatMessage({ id: 'stores.staff.removed' }) });
    onClose();
  };
  return (
    <Dialog
      open
      onClose={onClose}
      role="alertdialog"
      title={intl.formatMessage({ id: 'stores.staff.removeTitle' }, { name })}
      description={intl.formatMessage({ id: member.status === 'invited' ? 'stores.staff.removeInvite' : 'stores.staff.removeBody' })}
      closeLabel={intl.formatMessage({ id: 'stores.sheet.close' })}
      footer={
        <>
          <Button onClick={onClose}>{intl.formatMessage({ id: 'stores.cancel' })}</Button>
          <Button variant="danger" loading={action.pending} onClick={() => void confirm()}>
            {intl.formatMessage({ id: 'stores.staff.removeConfirm' })}
          </Button>
        </>
      }
    >
      <BizError error={action.error} />
    </Dialog>
  );
}
