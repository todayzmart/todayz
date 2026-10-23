import { createFileRoute } from '@tanstack/react-router';
import { ProductPage } from '../../product/ProductPage';

export const Route = createFileRoute('/s/$slug/i/$itemId')({
  component: ProductRoute,
});

function ProductRoute() {
  const { slug, itemId } = Route.useParams();
  // The key starts a new item at quantity one again.
  return <ProductPage key={`${slug}/${itemId}`} slug={slug} itemId={itemId} />;
}
