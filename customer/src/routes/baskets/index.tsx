import { createFileRoute } from '@tanstack/react-router';
import { BasketsPage } from '../../basket/BasketsPage';

export const Route = createFileRoute('/baskets/')({
  component: BasketsPage,
});
