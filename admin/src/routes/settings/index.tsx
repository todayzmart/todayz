import { createFileRoute } from '@tanstack/react-router';
import { SettingsPage } from '../../settings/SettingsPage';
import { validateSettingsSearch } from '../../settings/search';

export const Route = createFileRoute('/settings/')({ validateSearch: validateSettingsSearch, component: Settings });

function Settings() {
  return <SettingsPage search={Route.useSearch()} />;
}
