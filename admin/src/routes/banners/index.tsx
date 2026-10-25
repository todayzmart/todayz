import { createFileRoute } from '@tanstack/react-router';
import { BannersPage } from '../../banners/BannersPage';
import { validateBannersSearch } from '../../banners/search';

export const Route = createFileRoute('/banners/')({ validateSearch: validateBannersSearch, component: Banners });

function Banners() {
  return <BannersPage search={Route.useSearch()} />;
}
