import { acceptStaffInvite } from '@todayz/api-client/gen/business/business';
import type { StaffInvitation } from '@todayz/api-client/gen/model/staffInvitation';
import { Button, EmptyState, Skeleton, useToast } from '@todayz/ui';
import { useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { Inbox, MailQuestion } from 'lucide-react';
import { useIntl } from 'react-intl';
import { BizError } from './BizError';
import { businessesKey, useAction, useBusinesses } from './queries';
import styles from './stores.module.css';

/**
 * B-01 invite acceptance. The invited person is signed in by now (the shell sends signed-out visitors
 * to sign in and back). The API lists the invitations for the signed-in phone, so the link only has
 * to bring the person here: the invitation whose id is in the link comes first.
 * TODO(lead): 04 has no "get invitation by token" route; confirm the link carries the staff id.
 */
export function InvitePage({ token }: { token: string }) {
  const intl = useIntl();
  const q = useBusinesses();
  const invitations = q.data?.invitations ?? [];
  const first = invitations.find((i) => i.staffId === token);
  const rest = invitations.filter((i) => i !== first);
  return (
    <div className={styles.page}>
      <h1 className="tz-page-title">{intl.formatMessage({ id: 'stores.invite.title' })}</h1>
      {q.isLoading && (
        <div role="status" aria-label={intl.formatMessage({ id: 'stores.invite.loading' })}>
          <Skeleton height="6rem" />
        </div>
      )}
      {q.isError && (
        <EmptyState
          icon={Inbox}
          title={intl.formatMessage({ id: 'stores.invite.failed.title' })}
          description={intl.formatMessage({ id: 'stores.loadFailed.body' })}
          action={<Button onClick={() => void q.refetch()}>{intl.formatMessage({ id: 'stores.retry' })}</Button>}
        />
      )}
      {q.data && invitations.length === 0 && (
        <EmptyState
          icon={MailQuestion}
          title={intl.formatMessage({ id: 'stores.invite.none.title' })}
          description={intl.formatMessage({ id: 'stores.invite.none.body' })}
          action={
            <Link to="/" className="tz-btn">
              {intl.formatMessage({ id: 'stores.invite.home' })}
            </Link>
          }
        />
      )}
      {first && <InviteCard invitation={first} />}
      {rest.length > 0 && (
        <section className={styles.stack} aria-labelledby="more-h">
          {first && (
            <h2 id="more-h" className="tz-card-title">
              {intl.formatMessage({ id: 'stores.invite.others' })}
            </h2>
          )}
          {rest.map((i) => (
            <InviteCard key={i.staffId} invitation={i} />
          ))}
        </section>
      )}
    </div>
  );
}

function InviteCard({ invitation }: { invitation: StaffInvitation }) {
  const intl = useIntl();
  const qc = useQueryClient();
  const toast = useToast();
  const action = useAction();
  const navigate = useNavigate();
  const accept = async () => {
    const out = await action.run(`accept-${invitation.staffId}`, () => acceptStaffInvite(invitation.businessId, invitation.staffId));
    if (!out) return;
    await qc.invalidateQueries({ queryKey: businessesKey });
    toast.show({ message: intl.formatMessage({ id: 'stores.invite.accepted' }, { brand: invitation.brandName }) });
    await navigate({ to: '/' });
  };
  return (
    <section className="tz-card" aria-label={invitation.brandName}>
      <p className="tz-card-desc">
        {intl.formatMessage(
          { id: 'stores.invite.for' },
          { brand: invitation.brandName, role: intl.formatMessage({ id: `stores.staff.role.${invitation.role}` }).toLowerCase() },
        )}
      </p>
      <BizError error={action.error} />
      <div className={styles.actions}>
        <Button variant="primary" loading={action.pending} aria-label={intl.formatMessage({ id: 'stores.invite.acceptNamed' }, { brand: invitation.brandName })} onClick={() => void accept()}>
          {intl.formatMessage({ id: 'stores.invite.accept' })}
        </Button>
      </div>
    </section>
  );
}
