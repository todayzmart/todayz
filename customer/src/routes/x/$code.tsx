import { createFileRoute } from '@tanstack/react-router';
import { SharedLink } from '../../share/SharedLink';

export const Route = createFileRoute('/x/$code')({ component: ShareRoute });

function ShareRoute() {
  const { code } = Route.useParams();
  return <SharedLink key={code} code={code} />;
}
