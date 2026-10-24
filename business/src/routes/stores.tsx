import { createFileRoute } from '@tanstack/react-router';
import { StoresPage } from '../stores/StoresPage';

/** B-13 Stores and staff. A static route outranks `/$section`, so this replaces the placeholder. */
export const Route = createFileRoute('/stores')({
  component: StoresPage,
});
