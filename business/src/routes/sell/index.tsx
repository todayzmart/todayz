import { createFileRoute } from '@tanstack/react-router';
import { SellPage } from '../../pos/SellPage';

/** B-05 Sell. A static route outranks `/$section`, so this replaces the placeholder. */
export const Route = createFileRoute('/sell/')({
  component: SellPage,
});
