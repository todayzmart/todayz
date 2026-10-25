import { createFileRoute } from '@tanstack/react-router';
import { CustomersPage } from '../../customers/CustomersPage';
import { validateCustomersSearch } from '../../customers/search';

/** B-09 Customers; `segment=slipping` is the "See customers" insight link (D-99). */
export const Route = createFileRoute('/customers/')({
  validateSearch: validateCustomersSearch,
  component: function CustomersRoute() {
    return <CustomersPage search={Route.useSearch()} />;
  },
});
