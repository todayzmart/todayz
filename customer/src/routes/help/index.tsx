import { createFileRoute } from '@tanstack/react-router';
import { HelpPage } from '../../help/HelpPage';

/** C-19 Help: the customer's tickets. */
export const Route = createFileRoute('/help/')({
  component: HelpPage,
});
