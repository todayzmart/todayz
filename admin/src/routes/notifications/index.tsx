import { createFileRoute } from '@tanstack/react-router';
import { NotificationsPage } from '../../notifications/NotificationsPage';
import { validateNotificationsSearch } from '../../notifications/search';

export const Route = createFileRoute('/notifications/')({ validateSearch: validateNotificationsSearch, component: Notifications });

function Notifications() {
  return <NotificationsPage search={Route.useSearch()} />;
}
