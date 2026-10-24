import { createFileRoute } from '@tanstack/react-router';
import { ReviewsPage } from '../../reviews/ReviewsPage';

/** B-11 Reviews. A static route outranks `/$section`. */
export const Route = createFileRoute('/reviews/')({
  component: ReviewsPage,
});
