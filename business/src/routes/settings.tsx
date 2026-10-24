import { createFileRoute } from '@tanstack/react-router';
import { SettingsPage } from '../settings/SettingsPage';

/** B-14 Settings. A static route outranks `/$section`, so this replaces the placeholder. */
export const Route = createFileRoute('/settings')({
  component: SettingsPage,
});
