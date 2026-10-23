import { createFileRoute } from '@tanstack/react-router';
import { StorePage, type StoreState } from '../../store/StorePage';

const text = (v: unknown, max: number): string | undefined => (typeof v === 'string' && v.trim() ? v.trim().slice(0, max) : undefined);

export const Route = createFileRoute('/s/$slug/')({
  // `?r=` is the QR deep link's referral code (01 §3); it is kept on the device, never logged.
  validateSearch: (s: Record<string, unknown>): StoreState => {
    const out: StoreState = {};
    const q = text(s['q'], 120);
    if (q) out.q = q;
    const cat = text(s['cat'], 64);
    if (cat && /^[a-z0-9]+(-[a-z0-9]+)*$/.test(cat)) out.cat = cat;
    const r = text(s['r'], 64);
    if (r) out.r = r;
    return out;
  },
  component: StoreRoute,
});

function StoreRoute() {
  const { slug } = Route.useParams();
  const state = Route.useSearch();
  const navigate = Route.useNavigate();
  return <StorePage slug={slug} state={state} onChange={(next) => void navigate({ search: next, replace: true })} />;
}
