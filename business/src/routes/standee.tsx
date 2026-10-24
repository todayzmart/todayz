import { createFileRoute } from '@tanstack/react-router';
import { StandeePage } from '../standee/StandeePage';

/** QR standee: preview and A5 PDF (W2-10, D-51). A static route outranks `/$section`. */
export const Route = createFileRoute('/standee')({
  component: StandeePage,
});
