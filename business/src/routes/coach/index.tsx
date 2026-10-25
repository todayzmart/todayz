import { createFileRoute } from '@tanstack/react-router';
import { CoachPage } from '../../coach/CoachPage';

/** B-19 Shop coach. A static route outranks `/$section`. */
export const Route = createFileRoute('/coach/')({
  component: CoachPage,
});
