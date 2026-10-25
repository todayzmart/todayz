import { Button, EmptyState } from '@todayz/ui';
import { ShieldOff, TriangleAlert } from 'lucide-react';
import { useIntl } from 'react-intl';
import { ROLES, type Role } from '../app/nav/access';

const ROLE_LABEL: Record<Role, string> = {
  field_agent: 'shell.role.field_agent',
  support: 'shell.role.support',
  catalogue: 'shell.role.catalogue',
  analyst: 'shell.role.analyst',
  finance: 'shell.role.finance',
  admin: 'shell.role.admin',
  super_admin: 'shell.role.super_admin',
};

function isRole(r: string): r is Role {
  return (ROLES as readonly string[]).includes(r);
}

/** A sidebar section the caller's roles do not open (the API would answer 403). */
export function NoAccess({ roles }: { roles: readonly string[] }) {
  const intl = useIntl();
  const known = roles.filter(isRole);
  const names = known.map((r) => intl.formatMessage({ id: ROLE_LABEL[r] })).join(', ');
  return (
    <EmptyState
      icon={ShieldOff}
      headingLevel={2}
      title={intl.formatMessage({ id: 'shell.access.title' })}
      description={known.length ? intl.formatMessage({ id: 'shell.access.body' }, { roles: names }) : intl.formatMessage({ id: 'shell.access.noRoles' })}
    />
  );
}

/** The account (and so its roles) could not be loaded. */
export function AccessFailed({ retry }: { retry: () => void }) {
  const intl = useIntl();
  return (
    <EmptyState
      icon={TriangleAlert}
      headingLevel={2}
      title={intl.formatMessage({ id: 'shell.access.loadFailed' })}
      action={<Button onClick={retry}>{intl.formatMessage({ id: 'shell.access.retry' })}</Button>}
    />
  );
}
