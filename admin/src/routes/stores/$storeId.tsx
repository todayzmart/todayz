import { createFileRoute } from '@tanstack/react-router';
import { StorePage } from '../../stores/StorePage';

export const Route = createFileRoute('/stores/$storeId')({ component: Store });

function Store() {
  const { storeId } = Route.useParams();
  return <StorePage key={storeId} id={storeId} />;
}
