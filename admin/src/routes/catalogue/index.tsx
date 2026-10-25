import { createFileRoute } from '@tanstack/react-router';
import { CataloguePage } from '../../catalogue/CataloguePage';
import { validateCatalogueSearch } from '../../catalogue/search';

export const Route = createFileRoute('/catalogue/')({ validateSearch: validateCatalogueSearch, component: Catalogue });

function Catalogue() {
  return <CataloguePage search={Route.useSearch()} />;
}
