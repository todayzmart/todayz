import { createFileRoute } from '@tanstack/react-router';
import { AuditLogPage } from '../../audit/AuditLogPage';
import { validateAuditSearch } from '../../audit/search';

export const Route = createFileRoute('/audit/')({ validateSearch: validateAuditSearch, component: Audit });

function Audit() {
  return <AuditLogPage search={Route.useSearch()} />;
}
