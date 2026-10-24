import { createFileRoute } from '@tanstack/react-router';
import { CreditsPage } from '../../credits/CreditsPage';

/** B-10 Credits and referrals. A static route outranks `/$section`. */
export const Route = createFileRoute('/credits/')({
  component: CreditsPage,
});
