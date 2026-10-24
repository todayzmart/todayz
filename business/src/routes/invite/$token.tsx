import { createFileRoute } from '@tanstack/react-router';
import { InvitePage } from '../../stores/InvitePage';

/** B-01 invite acceptance for staff. Signed-out visitors are sent to sign in first and come back here. */
export const Route = createFileRoute('/invite/$token')({
  component: Invite,
});

function Invite() {
  const { token } = Route.useParams();
  return <InvitePage token={token} />;
}
