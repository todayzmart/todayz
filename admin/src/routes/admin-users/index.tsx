import { createFileRoute } from '@tanstack/react-router';
import { AdminUsersPage } from '../../admin-users/AdminUsersPage';
import { validateAdminUsersSearch } from '../../admin-users/search';

export const Route = createFileRoute('/admin-users/')({ validateSearch: validateAdminUsersSearch, component: AdminUsers });

function AdminUsers() {
  return <AdminUsersPage search={Route.useSearch()} />;
}
