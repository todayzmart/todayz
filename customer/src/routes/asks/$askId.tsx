import { createFileRoute } from '@tanstack/react-router';
import { AskPage } from '../../asks/AskPage';

export const Route = createFileRoute('/asks/$askId')({ component: AskRoute });

function AskRoute() {
  const { askId } = Route.useParams();
  return <AskPage key={askId} askId={askId} />;
}
