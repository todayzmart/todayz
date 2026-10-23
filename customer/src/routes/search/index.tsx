import { createFileRoute } from '@tanstack/react-router';
import { SearchPage, type SearchState, type SortKey } from '../../search/SearchPage';

const SORTS: readonly string[] = ['relevance', 'nearest', 'cheapest'];
const flag = (v: unknown): true | undefined => (v === true || v === 'true' ? true : undefined);

export const Route = createFileRoute('/search/')({
  validateSearch: (s: Record<string, unknown>): SearchState => {
    const out: SearchState = {};
    if (typeof s['q'] === 'string' && s['q'].trim()) out.q = s['q'].trim().slice(0, 120);
    if (typeof s['sort'] === 'string' && SORTS.includes(s['sort']) && s['sort'] !== 'relevance') out.sort = s['sort'] as SortKey;
    for (const f of ['open', 'pickup', 'delivery', 'offers'] as const) {
      const v = flag(s[f]);
      if (v) out[f] = v;
    }
    if (typeof s['category'] === 'string' && /^[a-z0-9]+(-[a-z0-9]+)*$/.test(s['category'])) out.category = s['category'];
    if (s['parse'] === false || s['parse'] === 'false') out.parse = false;
    return out;
  },
  component: SearchRoute,
});

function SearchRoute() {
  const state = Route.useSearch();
  const navigate = Route.useNavigate();
  return <SearchPage state={state} onChange={(next) => void navigate({ search: next, replace: true })} />;
}
