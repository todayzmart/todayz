import { createFileRoute } from '@tanstack/react-router';
import { BusinessPage } from '../../businesses/BusinessPage';

export const Route = createFileRoute('/businesses/$businessId')({ component: Business });

function Business() {
  const { businessId } = Route.useParams();
  return <BusinessPage key={businessId} id={businessId} />;
}
