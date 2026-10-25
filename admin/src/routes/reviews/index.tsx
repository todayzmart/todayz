import { createFileRoute } from '@tanstack/react-router';
import { ReviewsPage } from '../../reviews/ReviewsPage';
import { validateReviewsSearch } from '../../reviews/search';

export const Route = createFileRoute('/reviews/')({ validateSearch: validateReviewsSearch, component: Reviews });

function Reviews() {
  return <ReviewsPage search={Route.useSearch()} />;
}
