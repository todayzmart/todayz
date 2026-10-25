import { createFileRoute } from '@tanstack/react-router';
import { ShopPage } from '../../field/ShopPage';

export const Route = createFileRoute('/field/$businessId')({ component: Shop });

function Shop() {
  const { businessId } = Route.useParams();
  return <ShopPage key={businessId} id={businessId} />;
}
