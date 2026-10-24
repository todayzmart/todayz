import { createFileRoute } from '@tanstack/react-router';
import { CategoryPage } from '../category/CategoryPage';

export const Route = createFileRoute('/c/$slug')({
  component: CategoryRoute,
});

function CategoryRoute() {
  const { slug } = Route.useParams();
  return <CategoryPage slug={slug} />;
}
