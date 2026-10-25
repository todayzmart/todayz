import { createFileRoute } from '@tanstack/react-router';
import { RestockScanPage } from '../../restock-scan/RestockScanPage';

/** B-17 Scan supplier bill. A static route outranks `/$section`. */
export const Route = createFileRoute('/restock-scan/')({
  component: RestockScanPage,
});
