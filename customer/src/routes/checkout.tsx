import { createFileRoute } from '@tanstack/react-router';
import { CheckoutPage } from '../checkout/CheckoutPage';

export const Route = createFileRoute('/checkout')({
  component: CheckoutPage,
});
