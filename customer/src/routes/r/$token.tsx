import { createFileRoute } from '@tanstack/react-router';
import { ReceiptPage } from '../../receipt/ReceiptPage';

export const Route = createFileRoute('/r/$token')({ component: ReceiptRoute });

function ReceiptRoute() {
  const { token } = Route.useParams();
  return <ReceiptPage key={token} token={token} />;
}
