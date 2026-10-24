import { createFileRoute } from '@tanstack/react-router';
import { DeviceBasketPage } from '../../basket/BasketPage';

export const Route = createFileRoute('/baskets/device')({
  component: DeviceBasketPage,
});
