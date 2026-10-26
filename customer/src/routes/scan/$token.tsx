import { createFileRoute } from '@tanstack/react-router';
import { ScanBill } from '../../scan/ScanBill';

export const Route = createFileRoute('/scan/$token')({ component: ScanRoute });

function ScanRoute() {
  const { token } = Route.useParams();
  return <ScanBill key={token} token={token} />;
}
