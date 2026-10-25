import { createFileRoute } from '@tanstack/react-router';
import { CustomerPage } from '../../customers/CustomerPage';

export const Route = createFileRoute('/customers/$customerId')({ component: Customer });

function Customer() {
  const { customerId } = Route.useParams();
  return <CustomerPage key={customerId} id={customerId} />;
}
