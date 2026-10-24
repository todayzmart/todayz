import { createFileRoute } from '@tanstack/react-router';
import { SavedBasketPage } from '../../basket/BasketPage';

export const Route = createFileRoute('/baskets/$basketId')({
  component: BasketRoute,
});

function BasketRoute() {
  const { basketId } = Route.useParams();
  return <SavedBasketPage key={basketId} basketId={basketId} />;
}
