import { createFileRoute } from '@tanstack/react-router';
import { PulsePage } from '../pulse/PulsePage';
import { validatePulseSearch } from '../pulse/search';

export const Route = createFileRoute('/')({
  validateSearch: validatePulseSearch,
  component: Pulse,
});

function Pulse() {
  return <PulsePage search={Route.useSearch()} />;
}
